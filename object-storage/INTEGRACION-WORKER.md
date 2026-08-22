# Integración worker ↔ servicio de almacenamiento de objetos

Este documento describe **lo construido** en `object-storage/` y **cómo el worker (y
la API) se comunican con él por red**, sin compartir disco.

## 1. Lo que ya está hecho

Un servicio HTTP en Go, en `task_learning_01/object-storage/`, que sustituye a
MinIO/S3 con el mismo *contrato* (guardar/leer por clave). Componentes:

| Archivo | Rol |
|---------|-----|
| `internal/config/config.go` | Carga config desde el entorno (`STORAGE_ADDR`, `STORAGE_ROOT`, `STORAGE_TOKEN`, `MAX_OBJECT_BYTES`). |
| `internal/store/store.go` | Interfaz `Store` (Put/Get/Head/Delete/List) + errores. |
| `internal/store/fs.go` | Backend en carpeta: escritura atómica y rechazo de *path traversal*. |
| `internal/httpapi/server.go` | API HTTP: routing, auth Bearer, límite de tamaño, mapeo de errores. |
| `cmd/server/main.go` | Arranque con apagado ordenado. |
| `Dockerfile` | Imagen multi-stage, usuario no-root. |

### API expuesta

Todas las rutas `/v1` exigen `Authorization: Bearer <STORAGE_TOKEN>`.

| Método   | Ruta                        | Éxito | Descripción                     |
|----------|-----------------------------|-------|---------------------------------|
| `PUT`    | `/v1/objects/{clave...}`    | `201` | Guarda el objeto.               |
| `GET`    | `/v1/objects/{clave...}`    | `200` | Descarga el objeto.             |
| `HEAD`   | `/v1/objects/{clave...}`    | `200` | Metadatos sin cuerpo.           |
| `DELETE` | `/v1/objects/{clave...}`    | `204` | Borra el objeto.                |
| `GET`    | `/v1/objects?prefix={p}`    | `200` | Lista claves bajo un prefijo.   |
| `GET`    | `/healthz`                  | `200` | Salud (sin auth).               |

Errores: `400` clave inválida · `401` token ausente/incorrecto · `404` no
encontrado · `413` supera el tamaño máximo.

### Convención de claves

La ruta concreta **siempre viaja por la base de datos**; nadie la deduce.

```
originals/{owner_id}/{document_id}      la escribe la API,   la lee el worker
bundles/{owner_id}/{bundle_id}/...      la escribe el worker, la lee la API
```

## 2. Cómo se comunica el worker

El núcleo del worker no conoce HTTP. Depende de un **puerto** (interfaz) que hoy
está declarado así en `worker/internal/adapter/objectstore/objectstore.go`:

```go
type Store interface {
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}
```

La integración consiste en **una nueva implementación de esa interfaz** que, en vez
de tocar una carpeta local (`fs.go`) o MinIO (`minio.go`), hace peticiones HTTP a
este servicio. La llamamos `HTTPStore`. El resto del worker no cambia.

### Diagrama

```
                    ┌──────────────────────────┐
   Worker (Go)      │  objectstore.HTTPStore    │        Servicio object-storage
 ┌───────────┐      │  (implementa Store)       │        ┌────────────────────┐
 │ processor │─Get/─┤  GET  /v1/objects/{key} ──┼──HTTP──▶  httpapi.Server     │
 │           │ Put  │  PUT  /v1/objects/{key} ──┼──HTTP──▶  store.FSStore       │
 └───────────┘      └──────────────────────────┘        │   └─► /data/objects  │
                       Authorization: Bearer …           └────────────────────┘
```

### Mapeo puerto → HTTP

| Método del puerto | Petición HTTP                          | Respuesta esperada           |
|-------------------|----------------------------------------|------------------------------|
| `Get(key)`        | `GET /v1/objects/{key}` + Bearer       | `200` → cuerpo; `404` → `ErrNotFound` |
| `Put(key, r, …)`  | `PUT /v1/objects/{key}` + Bearer, cuerpo = `r`, cabecera `Content-Type` | `201` → `nil` |

### Boceto del adaptador `HTTPStore`

Archivo sugerido: `worker/internal/adapter/objectstore/http.go`.

```go
package objectstore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HTTPStore implementa Store hablando con el servicio object-storage por HTTP.
type HTTPStore struct {
	baseURL string       // p. ej. http://object-storage:9000
	token   string       // STORAGE_TOKEN compartido
	client  *http.Client
}

func NewHTTP(baseURL, token string) *HTTPStore {
	return &HTTPStore{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{},
	}
}

// objectURL escapa cada segmento de la clave pero conserva las barras como
// separadores de ruta.
func (s *HTTPStore) objectURL(key string) string {
	var b strings.Builder
	b.WriteString(s.baseURL)
	b.WriteString("/v1/objects")
	for _, seg := range strings.Split(key, "/") {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(seg))
	}
	return b.String()
}

func (s *HTTPStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Body, nil // el llamador cierra el cuerpo
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNotFound // el centinela que el núcleo ya entiende
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("object-storage GET %s: estado %d", key, resp.StatusCode)
	}
}

func (s *HTTPStore) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key), r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// Con size >= 0 se envía Content-Length; con -1 el cliente usa chunked.
	if size >= 0 {
		req.ContentLength = size
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("object-storage PUT %s: estado %d", key, resp.StatusCode)
	}
	return nil
}
```

> `ErrNotFound` ya existe en el paquete `objectstore` del worker; el `HTTPStore`
> solo lo reutiliza, así el núcleo distingue "no está" de "falló la red" sin saber
> nada de HTTP.

### Selección del backend

En `objectstore.go` del worker, `New(cfg)` elige el backend según una variable de
entorno. Basta añadir un caso:

```go
switch cfg.StorageBackend {
case "fs":
	return NewFS(cfg.StorageDir)
case "minio":
	return NewMinio(cfg)
case "http": // ← nuevo
	return NewHTTP(cfg.StorageBaseURL, cfg.StorageToken), nil
}
```

### Configuración del worker (nuevas variables)

| Variable             | Ejemplo                          | Descripción                          |
|----------------------|----------------------------------|--------------------------------------|
| `STORAGE_BACKEND`    | `http`                           | Selecciona el adaptador `HTTPStore`. |
| `STORAGE_BASE_URL`   | `http://object-storage:9000`     | URL del servicio (nombre del servicio en Compose). |
| `STORAGE_TOKEN`      | `secreto`                        | Mismo token que exige el servicio.   |

## 3. Detalles importantes

- **El original es entrada no confiable.** Al leerlo en el procesador, envolver con
  `io.LimitReader(rc, MaxInputSize)` antes de parsear.
- **Orden al publicar el bundle:** escribir primero los objetos con `Put` y solo
  después hacer el `COMMIT` en la base de datos. Un objeto huérfano es inofensivo;
  una fila que apunta a un objeto inexistente, no.
- **Idempotencia:** repetir un `PUT` sobre la misma clave sobrescribe de forma
  atómica; reprocesar un trabajo no crea duplicados en el almacén.
- **Mismo token** en el worker, la API y el servicio (una sola variable compartida
  en Compose).

## 4. Cómo probar la comunicación

Con el servicio corriendo (local o en Docker), el worker en modo `http` hará `GET`
y `PUT` reales. Para probarlo de forma aislada, simula al worker con curl:

```bash
# El worker escribe un archivo del bundle (equivale a Put):
printf '# Concepto' | curl -s -X PUT -H 'Authorization: Bearer secreto' \
  --data-binary @- http://localhost:9000/v1/objects/bundles/u1/b1/index.md

# El worker lee el original (equivale a Get):
curl -s -H 'Authorization: Bearer secreto' \
  http://localhost:9000/v1/objects/originals/u1/doc-123
```
