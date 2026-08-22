package ports

import (
	"context"
	"io"
)

// ObjectStore abstrae el almacenamiento de objetos que usa la API. Solo
// expone lo que la API necesita: subir el original al cargar el documento,
// leer el bundle ya generado para la descarga, y borrar ambos.
//
// Leer el original (GetOriginal) y escribir los archivos del bundle
// (PutBundleFile) eran operaciones del pipeline de conversión; se quitaron de
// este puerto porque ahora las hace el worker (módulo aparte, ../worker)
// contra su propio object store, no la API.
type ObjectStore interface {
	PutOriginal(ctx context.Context, key string, r io.Reader, size int64, contentType string) error

	// GetBundleStream entrega el bundle empaquetado para descarga en streaming.
	GetBundleStream(ctx context.Context, bundleKey string) (io.ReadCloser, error)

	DeleteOriginal(ctx context.Context, key string) error
	// DeleteBundle borra todos los objetos bajo el prefijo bundleKey.
	DeleteBundle(ctx context.Context, bundleKey string) error
}
