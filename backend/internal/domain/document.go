package domain

import "time"

type Document struct {
	ID         string
	OwnerID    string
	Filename   string
	Format     string // "markdown" | "html" | "text"
	StorageKey string // ubicación del original en el almacenamiento de objetos
	SizeBytes  int64
	CreatedAt  time.Time
}
