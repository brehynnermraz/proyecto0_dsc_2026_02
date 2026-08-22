package domain

import (
	"encoding/json"
	"time"
)

// JobStatus es el vocabulario de estados del WORKER (../worker), que es la
// fuente de verdad: la base de datos usa este enum (ver migrations del worker).
// La API no lo cambia; solo lo traduce al vocabulario del frontend al responder
// (ver Frontend()).
type JobStatus string

const (
	JobQueued     JobStatus = "queued"     // creado por la API, mensaje publicado
	JobProcessing JobStatus = "processing" // un worker lo tomó
	JobSucceeded  JobStatus = "succeeded"  // bundle publicado
	JobFailed     JobStatus = "failed"     // fallo reintentable
	JobCancelled  JobStatus = "cancelled"  // lo canceló el usuario
	JobDead       JobStatus = "dead"       // agotó max_attempts; terminal
)

// Frontend traduce el estado del worker a los cuatro que el frontend entiende
// y muestra: pending | processing | done | failed. Cualquier estado terminal
// que no sea "succeeded" (failed, cancelled, dead) se presenta como "failed",
// porque para el usuario el resultado es el mismo: no hay bundle que descargar.
func (s JobStatus) Frontend() string {
	switch s {
	case JobQueued:
		return "pending"
	case JobProcessing:
		return "processing"
	case JobSucceeded:
		return "done"
	default:
		return "failed"
	}
}

// Terminal indica si el job ya no cambiará de estado. Se usa para cerrar el
// stream SSE.
func (s JobStatus) Terminal() bool {
	switch s {
	case JobSucceeded, JobFailed, JobCancelled, JobDead:
		return true
	default:
		return false
	}
}

// JobSummary es una fila de la lista de trabajos del usuario (GET /jobs): lo
// mínimo que el dashboard necesita para pintar la tabla. El estado en vivo y el
// bundle los trae luego cada fila con GET /jobs/:id.
type JobSummary struct {
	ID        string
	Filename  string
	SizeBytes int64
	CreatedAt time.Time
}

// Job es lo que la API conoce de un trabajo: una fila de `jobs` más, si ya
// terminó con éxito, el id de su bundle (que NO es columna de `jobs` en el
// esquema del worker, sino que se resuelve por bundles.job_id).
type Job struct {
	ID         string
	DocumentID string
	OwnerID    string
	Status     JobStatus
	Attempts   int
	Error      string
	BundleID   *string
	CreatedAt  time.Time
}

// MarshalJSON presenta el job al frontend ya traducido: Status sale en el
// vocabulario del frontend (pending|processing|done|failed), no en el del
// worker. Así los handlers pueden hacer c.JSON(job) sin traducir a mano y el
// frontend nunca ve "succeeded"/"queued", que no sabría pintar.
func (j Job) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID         string    `json:"ID"`
		DocumentID string    `json:"DocumentID"`
		OwnerID    string    `json:"OwnerID"`
		Status     string    `json:"Status"`
		Attempts   int       `json:"Attempts"`
		Error      string    `json:"Error"`
		BundleID   *string   `json:"BundleID"`
		CreatedAt  time.Time `json:"CreatedAt"`
	}{
		ID:         j.ID,
		DocumentID: j.DocumentID,
		OwnerID:    j.OwnerID,
		Status:     j.Status.Frontend(),
		Attempts:   j.Attempts,
		Error:      j.Error,
		BundleID:   j.BundleID,
		CreatedAt:  j.CreatedAt,
	})
}
