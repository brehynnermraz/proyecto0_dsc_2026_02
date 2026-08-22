package domain

import "time"

// Bundle es la entidad que la API conoce del resultado de una conversión: la
// fila en `bundles` que apunta a los archivos en el object store. La API solo
// lo lee (para autorizar y para la descarga) y lo borra; quien lo construye es
// el worker (../worker), que es la fuente de verdad.
//
// StoragePrefix es la carpeta del bundle en el object store
// (bundles/{owner_id}/{bundle_id}); bajo ese prefijo viven index.md, log.md y
// los conceptos. La descarga lista ese prefijo y lo empaqueta en zip.
type Bundle struct {
	ID            string
	JobID         string
	OwnerID       string
	StoragePrefix string
	CreatedAt     time.Time
}
