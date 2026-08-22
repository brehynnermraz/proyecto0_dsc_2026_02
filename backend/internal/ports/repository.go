package ports

import (
	"context"

	"okfbundler/internal/domain"
)

type UserRepository interface {
	Create(ctx context.Context, u *domain.User) error
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
}

type DocumentRepository interface {
	Create(ctx context.Context, d *domain.Document) error
	FindByID(ctx context.Context, id string) (*domain.Document, error)
	Delete(ctx context.Context, id string) error
}

// JobRepository es SOLO de lectura y alta desde la API. Reclamar el trabajo,
// renovar el lease, marcar succeeded/failed/dead: todo eso lo hace el worker
// (../worker) directamente sobre Postgres, que es la fuente de verdad. La API
// crea el job al subir el documento y lo consulta para el estado y la
// autorización; el borrado lo hace por cascada al borrar el documento.
type JobRepository interface {
	Create(ctx context.Context, j *domain.Job) error

	// FindByID trae el job y, si ya produjo un bundle, su id (resuelto por
	// bundles.job_id, porque el esquema del worker no guarda jobs.bundle_id).
	FindByID(ctx context.Context, id string) (*domain.Job, error)

	// ListByOwner devuelve los trabajos del usuario (los más recientes primero),
	// con el nombre y tamaño del documento para pintar la tabla del dashboard.
	// Es la fuente de la lista: sustituye al historial en localStorage del
	// frontend, que era por-navegador (no se veía en incógnito ni en otro equipo).
	ListByOwner(ctx context.Context, ownerID string) ([]domain.JobSummary, error)
}

type BundleRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Bundle, error)
	FindByJob(ctx context.Context, jobID string) (*domain.Bundle, error)
}
