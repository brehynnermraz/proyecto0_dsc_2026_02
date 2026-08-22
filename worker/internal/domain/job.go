// job.go — tipos del dominio relacionados con el trabajo y su resultado. Son los que
// ve la lógica de negocio (package job), independientes de cómo los guarda Postgres.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// JobStatus refleja el enum job_status de Postgres (ver migrations/0001_init.sql).
type JobStatus string

const (
	StatusQueued     JobStatus = "queued"
	StatusProcessing JobStatus = "processing"
	StatusSucceeded  JobStatus = "succeeded"
	StatusFailed     JobStatus = "failed"
	StatusCancelled  JobStatus = "cancelled"
	StatusDead       JobStatus = "dead"
)

// ValidationStatus refleja el enum validation_status de Postgres.
type ValidationStatus string

const (
	ValidationValid             ValidationStatus = "valid"
	ValidationValidWithWarnings ValidationStatus = "valid_with_warnings"
	ValidationInvalid           ValidationStatus = "invalid"
)

// SourceDocument son los metadatos del original que el worker necesita para procesarlo.
// StorageKey SIEMPRE viaja por la base de datos: el worker nunca deduce rutas, las lee.
type SourceDocument struct {
	ID         uuid.UUID
	Filename   string
	MIME       string
	SizeBytes  int64
	StorageKey string
}

// ClaimedJob es lo que devuelve Claim: el trabajo tomado en exclusiva más su documento,
// en un solo viaje a la base de datos.
type ClaimedJob struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	Attempts    int32
	MaxAttempts int32
	Document    SourceDocument
	ClaimedAt   time.Time
}

// Bundle son los metadatos que se insertan en la tabla bundles al publicar.
// StoragePrefix es la ruta bajo la que quedaron los archivos en el object store.
type Bundle struct {
	ID            uuid.UUID
	JobID         uuid.UUID
	OwnerID       uuid.UUID
	StoragePrefix string
	Validation    ValidationStatus
	Warnings      []string
	ConceptCount  int32
	SizeBytes     int64
}

// JobChange es la notificación best-effort del worker hacia la API (para empujar el
// cambio al navegador). Cuando llega, la base de datos ya tiene la verdad.
type JobChange struct {
	JobID    uuid.UUID  `json:"job_id"`
	Status   JobStatus  `json:"status"`
	BundleID *uuid.UUID `json:"bundle_id,omitempty"`
	Error    string     `json:"error,omitempty"`
}
