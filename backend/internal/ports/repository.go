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

type JobRepository interface {
	Create(ctx context.Context, j *domain.Job) error
	FindByID(ctx context.Context, id string) (*domain.Job, error)

	// ClaimForProcessing transiciona atómicamente un job de "pending" o
	// "failed" a "processing" — "failed" se incluye para que un reintento
	// tras un fallo transitorio pueda reprocesar el job. Devuelve ok=false
	// si el job ya está "processing" o "done" por otra entrega — así una
	// entrega duplicada de la cola (sección 6: "ausencia de duplicados") no
	// vuelve a procesar nada mientras la original está en curso o ya
	// terminó con éxito.
	ClaimForProcessing(ctx context.Context, id string) (ok bool, err error)

	MarkDone(ctx context.Context, id string, bundleID string) error
	MarkFailed(ctx context.Context, id string, attempt int, errMsg string) error

	// ClearBundle rompe la referencia jobs.bundle_id -> bundles.id, paso
	// obligatorio antes de borrar un bundle (ver DeleteJobService).
	ClearBundle(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

type BundleRepository interface {
	Create(ctx context.Context, b *domain.Bundle) error
	FindByID(ctx context.Context, id string) (*domain.Bundle, error)
	Delete(ctx context.Context, id string) error
}
