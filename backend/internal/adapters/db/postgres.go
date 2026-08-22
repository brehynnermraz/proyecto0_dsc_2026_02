package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

// Los repositorios de este archivo hablan contra el ESQUEMA DEL WORKER
// (task_learning_01/migrations/), que es la fuente de verdad compartida. Por
// eso `documents` tiene `mime` (no `format`), `jobs` usa el enum del worker
// (queued/processing/succeeded/...) sin columna `bundle_id`, y el vínculo
// job→bundle se resuelve por `bundles.job_id`.

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
		`INSERT INTO documents (id, owner_id, filename, mime, size_bytes, storage_key, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,now())`,
		d.ID, d.OwnerID, d.Filename, d.MIME, d.SizeBytes, d.StorageKey)
	return err
}

func (r *DocumentRepo) FindByID(ctx context.Context, id string) (*domain.Document, error) {
	var d domain.Document
	err := r.pool.QueryRow(ctx,
		`SELECT id, owner_id, filename, mime, storage_key, size_bytes, created_at
		 FROM documents WHERE id=$1`, id,
	).Scan(&d.ID, &d.OwnerID, &d.Filename, &d.MIME, &d.StorageKey, &d.SizeBytes, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Delete borra el documento. Por las FKs ON DELETE CASCADE del esquema del
// worker (jobs.document_id -> documents, bundles.job_id -> jobs), esto arrastra
// el job y su bundle en una sola sentencia.
func (r *DocumentRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM documents WHERE id=$1`, id)
	return err
}

// ---- trabajos ----

type JobRepo struct{ pool *pgxpool.Pool }

func NewJobRepo(pool *pgxpool.Pool) *JobRepo { return &JobRepo{pool: pool} }

var _ ports.JobRepository = (*JobRepo)(nil)

// Create inserta el job dejando que el esquema aplique los defaults del worker:
// status='queued', attempts=0, max_attempts=3.
func (r *JobRepo) Create(ctx context.Context, j *domain.Job) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO jobs (id, document_id, owner_id) VALUES ($1,$2,$3)`,
		j.ID, j.DocumentID, j.OwnerID)
	return err
}

// FindByID trae el job y, con un LEFT JOIN, el id de su bundle si ya existe.
// Ese bundle_id es lo que el frontend usa para habilitar la descarga.
func (r *JobRepo) FindByID(ctx context.Context, id string) (*domain.Job, error) {
	var j domain.Job
	var status string
	var errMsg *string
	var bundleID *string

	err := r.pool.QueryRow(ctx,
		`SELECT j.id, j.document_id, j.owner_id, j.status, j.attempts, j.error, b.id, j.created_at
		 FROM jobs j
		 LEFT JOIN bundles b ON b.job_id = j.id
		 WHERE j.id=$1`, id,
	).Scan(&j.ID, &j.DocumentID, &j.OwnerID, &status, &j.Attempts, &errMsg, &bundleID, &j.CreatedAt)
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

// ListByOwner lista los trabajos del usuario (recientes primero) con el nombre
// y tamaño del documento, para la tabla del dashboard.
func (r *JobRepo) ListByOwner(ctx context.Context, ownerID string) ([]domain.JobSummary, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT j.id, d.filename, d.size_bytes, j.created_at
		 FROM jobs j
		 JOIN documents d ON d.id = j.document_id
		 WHERE j.owner_id = $1
		 ORDER BY j.created_at DESC
		 LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.JobSummary
	for rows.Next() {
		var s domain.JobSummary
		if err := rows.Scan(&s.ID, &s.Filename, &s.SizeBytes, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- bundles ----

type BundleRepo struct{ pool *pgxpool.Pool }

func NewBundleRepo(pool *pgxpool.Pool) *BundleRepo { return &BundleRepo{pool: pool} }

var _ ports.BundleRepository = (*BundleRepo)(nil)

func (r *BundleRepo) FindByID(ctx context.Context, id string) (*domain.Bundle, error) {
	return r.scanOne(ctx,
		`SELECT id, job_id, owner_id, storage_prefix, published_at FROM bundles WHERE id=$1`, id)
}

func (r *BundleRepo) FindByJob(ctx context.Context, jobID string) (*domain.Bundle, error) {
	return r.scanOne(ctx,
		`SELECT id, job_id, owner_id, storage_prefix, published_at FROM bundles WHERE job_id=$1`, jobID)
}

func (r *BundleRepo) scanOne(ctx context.Context, sql, arg string) (*domain.Bundle, error) {
	var b domain.Bundle
	err := r.pool.QueryRow(ctx, sql, arg).
		Scan(&b.ID, &b.JobID, &b.OwnerID, &b.StoragePrefix, &b.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}
