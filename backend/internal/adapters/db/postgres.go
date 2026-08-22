package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, dsn)
}

// ---- usuarios ----

type UserRepo struct{ pool *pgxpool.Pool }

func NewUserRepo(pool *pgxpool.Pool) *UserRepo { return &UserRepo{pool: pool} }

var _ ports.UserRepository = (*UserRepo)(nil)

func (r *UserRepo) Create(ctx context.Context, u *domain.User) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES ($1,$2,$3,now())`,
		u.ID, u.Email, u.PasswordHash)
	return err
}

func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var u domain.User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ---- documentos ----

type DocumentRepo struct{ pool *pgxpool.Pool }

func NewDocumentRepo(pool *pgxpool.Pool) *DocumentRepo { return &DocumentRepo{pool: pool} }

var _ ports.DocumentRepository = (*DocumentRepo)(nil)

func (r *DocumentRepo) Create(ctx context.Context, d *domain.Document) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO documents (id, owner_id, filename, format, storage_key, size_bytes, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,now())`,
		d.ID, d.OwnerID, d.Filename, d.Format, d.StorageKey, d.SizeBytes)
	return err
}

func (r *DocumentRepo) FindByID(ctx context.Context, id string) (*domain.Document, error) {
	var d domain.Document
	err := r.pool.QueryRow(ctx,
		`SELECT id, owner_id, filename, format, storage_key, size_bytes, created_at
		 FROM documents WHERE id=$1`, id,
	).Scan(&d.ID, &d.OwnerID, &d.Filename, &d.Format, &d.StorageKey, &d.SizeBytes, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DocumentRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM documents WHERE id=$1`, id)
	return err
}

// ---- trabajos ----

type JobRepo struct{ pool *pgxpool.Pool }

func NewJobRepo(pool *pgxpool.Pool) *JobRepo { return &JobRepo{pool: pool} }

var _ ports.JobRepository = (*JobRepo)(nil)

func (r *JobRepo) Create(ctx context.Context, j *domain.Job) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO jobs (id, document_id, owner_id, status, attempts, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,0,now(),now())`,
		j.ID, j.DocumentID, j.OwnerID, string(domain.JobPending))
	return err
}

func (r *JobRepo) FindByID(ctx context.Context, id string) (*domain.Job, error) {
	var j domain.Job
	var status string
	var errMsg *string
	var bundleID *string

	err := r.pool.QueryRow(ctx,
		`SELECT id, document_id, owner_id, status, attempts, error, bundle_id, created_at, updated_at
		 FROM jobs WHERE id=$1`, id,
	).Scan(&j.ID, &j.DocumentID, &j.OwnerID, &status, &j.Attempts, &errMsg, &bundleID, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}

	j.Status = domain.JobStatus(status)
	j.BundleID = bundleID
	if errMsg != nil {
		j.Error = *errMsg
	}
	return &j, nil
}

// ClaimForProcessing es el guardián de idempotencia: solo la entrega que
// gana este UPDATE atómico llega a ejecutar la conversión.
//
// También reclama jobs en "failed": MarkFailed dejó ese status tras un
// intento fallido, y el mensaje que RabbitMQ reentrega desde jobs.retry
// necesita poder reclamarlo de nuevo para que el reintento con backoff
// realmente vuelva a ejecutar la conversión (si solo aceptara "pending", un
// job fallido una vez quedaría atascado: la entrega reintentada encontraría
// status="failed", ok=false, y el Consumer haría Ack sin reprocesar nada).
// Un job en "processing" queda fuera a propósito, así una entrega
// concurrente de la MISMA entrega en curso no se le adelanta.
func (r *JobRepo) ClaimForProcessing(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status=$1, updated_at=now() WHERE id=$2 AND status IN ($3, $4)`,
		string(domain.JobProcessing), id, string(domain.JobPending), string(domain.JobFailed))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *JobRepo) MarkDone(ctx context.Context, id string, bundleID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status=$1, bundle_id=$2, updated_at=now() WHERE id=$3`,
		string(domain.JobDone), bundleID, id)
	return err
}

func (r *JobRepo) MarkFailed(ctx context.Context, id string, attempt int, errMsg string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status=$1, attempts=$2, error=$3, updated_at=now() WHERE id=$4`,
		string(domain.JobFailed), attempt, errMsg, id)
	return err
}

// ClearBundle rompe la referencia jobs.bundle_id -> bundles.id. Es un paso
// obligatorio antes de borrar el bundle: la FK circular entre jobs y
// bundles (0001_init.sql) impide borrar la fila de bundles mientras un job
// todavía le apunte.
func (r *JobRepo) ClearBundle(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE jobs SET bundle_id=NULL, updated_at=now() WHERE id=$1`, id)
	return err
}

func (r *JobRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, id)
	return err
}

// ---- bundles ----

type BundleRepo struct{ pool *pgxpool.Pool }

func NewBundleRepo(pool *pgxpool.Pool) *BundleRepo { return &BundleRepo{pool: pool} }

var _ ports.BundleRepository = (*BundleRepo)(nil)

func (r *BundleRepo) Create(ctx context.Context, b *domain.Bundle) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO bundles (id, job_id, owner_id, storage_key, valid, created_at)
		 VALUES ($1,$2,$3,$4,$5,now())`,
		b.ID, b.JobID, b.OwnerID, b.StorageKey, b.Valid)
	return err
}

func (r *BundleRepo) FindByID(ctx context.Context, id string) (*domain.Bundle, error) {
	var b domain.Bundle
	err := r.pool.QueryRow(ctx,
		`SELECT id, job_id, owner_id, storage_key, valid, created_at FROM bundles WHERE id=$1`, id,
	).Scan(&b.ID, &b.JobID, &b.OwnerID, &b.StorageKey, &b.Valid, &b.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *BundleRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM bundles WHERE id=$1`, id)
	return err
}
