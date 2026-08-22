# object-storage

Servicio de almacenamiento de objetos propio, escrito en Go, para la plataforma de
conversión a bundles OKF. Guarda **originales** y **bundles** fuera del disco
efímero de los contenedores y los expone por una API HTTP sencilla, de modo que la
API y los workers accedan a ellos por red y no por un volumen compartido.

Es un sustituto de MinIO/S3 construido desde cero: mismo *contrato* (PUT/GET por
clave), sin dependencias externas (solo la librería estándar de Go).

## Convención de claves

La clave es la ruta del objeto y viaja siempre por la base de datos; el servicio no
deduce rutas, solo las recibe.

```
originals/{owner_id}/{document_id}      escribe la API   · lee el worker
bundles/{owner_id}/{bundle_id}/...      escribe el worker · lee la API
```

## API

Todas las rutas bajo `/v1` exigen `Authorization: Bearer <STORAGE_TOKEN>`.

| Método   | Ruta                          | Descripción                                  | Éxito |
|----------|-------------------------------|----------------------------------------------|-------|
| `PUT`    | `/v1/objects/{clave...}`      | Guarda el objeto (cuerpo = contenido)        | `201` |
| `GET`    | `/v1/objects/{clave...}`      | Descarga el objeto                           | `200` |
| `HEAD`   | `/v1/objects/{clave...}`      | Metadatos (tamaño, tipo) sin cuerpo          | `200` |
| `DELETE` | `/v1/objects/{clave...}`      | Borra el objeto                              | `204` |
| `GET`    | `/v1/objects?prefix={p}`      | Lista las claves bajo un prefijo (JSON)      | `200` |
| `GET`    | `/healthz`                    | Sonda de salud (sin autenticación)           | `200` |

Errores: `400` clave inválida, `401` token ausente/incorrecto, `404` no encontrado,
`413` supera el tamaño máximo.

## Configuración (variables de entorno)

| Variable           | Obligatoria | Defecto          | Descripción                              |
|--------------------|-------------|------------------|------------------------------------------|
| `STORAGE_TOKEN`    | sí          | —                | Secreto Bearer que protege `/v1`.        |
| `STORAGE_ADDR`     | no          | `:9000`          | Dirección `host:puerto` de escucha.      |
| `STORAGE_ROOT`     | no          | `/data/objects`  | Carpeta raíz de los objetos.             |
| `MAX_OBJECT_BYTES` | no          | `52428800` (50 MiB) | Tamaño máximo por objeto.             |

## Ejecución local

```bash
STORAGE_TOKEN=secreto STORAGE_ROOT=/tmp/okf STORAGE_ADDR=:9099 go run ./cmd/server
```

Prueba rápida:

```bash
printf 'hola' | curl -s -X PUT -H 'Authorization: Bearer secreto' \
  --data-binary @- http://localhost:9099/v1/objects/bundles/u1/b1/index.md
curl -s -H 'Authorization: Bearer secreto' \
  http://localhost:9099/v1/objects/bundles/u1/b1/index.md
```

## Docker

```bash
docker build -t okf-object-storage .
docker run --rm -p 9000:9000 -e STORAGE_TOKEN=secreto \
  -v okf-objects:/data okf-object-storage
```

Sonda de salud para Compose (Alpine trae `wget`):

```yaml
healthcheck:
  test: ["CMD", "wget", "-qO-", "http://localhost:9000/healthz"]
  interval: 10s
  timeout: 3s
  retries: 5
```

## Tests

```bash
go test ./...
```

Cubren: ida y vuelta PUT/GET, sobrescritura idempotente, `HEAD`/`DELETE`, listado
por prefijo, rechazo de *path traversal*, autenticación, `404` y `413`.
