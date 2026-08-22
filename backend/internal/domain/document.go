package domain

import "time"

type Document struct {
	ID       string
	OwnerID  string
	Filename string
	// MIME es el tipo del original. Es lo que el worker (../worker) lee para
	// elegir el parser (documents.mime en el esquema del worker). La API lo
	// deriva del "format" que envía el frontend (ver app.mimeForFormat).
	MIME       string
	StorageKey string // ubicación del original en el object store
	SizeBytes  int64
	CreatedAt  time.Time
}
