package ports

import (
	"context"
	"io"
)

// ObjectStore abstrae el almacenamiento de objetos (MinIO en el diagrama).
// Los originales y los bundles nunca tocan el disco de la API ni del worker.
type ObjectStore interface {
	PutOriginal(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	GetOriginal(ctx context.Context, key string) (io.ReadCloser, error)

	// PutBundleFile escribe un archivo (index.md, log.md, un concepto...)
	// dentro del prefijo bundleKey.
	PutBundleFile(ctx context.Context, bundleKey, relativePath string, r io.Reader, size int64) error

	// GetBundleStream entrega el bundle empaquetado para descarga en streaming.
	GetBundleStream(ctx context.Context, bundleKey string) (io.ReadCloser, error)

	DeleteOriginal(ctx context.Context, key string) error
	// DeleteBundle borra todos los objetos bajo el prefijo bundleKey.
	DeleteBundle(ctx context.Context, bundleKey string) error
}
