// Package objectstore es el adaptador secundario de almacenamiento de objetos.
// Implementa el puerto Store (leer el original, escribir el bundle). Nada de esto
// entra en converter/: es infraestructura pura.
//
//	objectstore.go — interfaz Store, centinela ErrNotFound y el constructor New(cfg)
//	                 que elige backend según STORAGE_BACKEND.
//	fs.go          — backend en carpeta local (pruebas sin levantar el servicio).
//	http.go        — backend que habla por red con el servicio object-storage.
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/uniandes-isis4426/okf/worker/internal/config"
)

// ErrNotFound lo devuelve un backend cuando la clave no existe. Es el centinela que
// permite al núcleo distinguir "el original no está" (permanente: reintentar no lo
// hará aparecer) de "falló la red o el disco" (transitorio). Los backends lo mapean
// desde su fuente: fs desde os.IsNotExist, http desde un 404.
var ErrNotFound = errors.New("objeto no encontrado")

// Store es el puerto de almacenamiento de objetos.
//
// Claves (la ruta concreta SIEMPRE viaja por la base de datos, nunca se deduce):
//
//	originals/{owner_id}/{document_id}   la escribe la api, la lee el worker
//	bundles/{owner_id}/{bundle_id}/...   la escribe el worker, la lee la api
//
// Dos detalles: envolver la lectura con io.LimitReader(r, MaxInputSize) — el original
// es entrada no confiable; y escribir primero en el store y luego el COMMIT (un objeto
// huérfano es inofensivo; una fila sin objeto no).
type Store interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

// New elige el backend concreto según cfg.StorageBackend. Es lo que enchufan las
// composition roots (cmd/worker y cmd/seed) para hablar siempre con el mismo almacén.
func New(cfg config.Config) (Store, error) {
	switch cfg.StorageBackend {
	case "fs":
		return NewFS(cfg.StorageDir), nil
	case "http":
		return NewHTTP(cfg.StorageBaseURL, cfg.StorageToken), nil
	default:
		return nil, fmt.Errorf("STORAGE_BACKEND desconocido: %q (use fs|http)", cfg.StorageBackend)
	}
}
