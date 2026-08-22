// Package db es el adaptador secundario hacia Postgres (pgx/v5). Implementa el puerto
// port.JobRepository escribiendo el SQL a mano (sin sqlc, para que se lea línea por
// línea). Las consultas de referencia también están, como documentación del contrato,
// en queries/*.sql.
//
// Lo que de verdad importa aquí es la TRADUCCIÓN DE ERRORES: si Claim con cero filas
// no se convierte en job.ErrNotClaimable, el receptor toma la decisión de ack/nack
// equivocada y una reentrega inofensiva se vuelve un bucle de reintentos.
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
	"github.com/uniandes-isis4426/okf/worker/internal/job"
	"github.com/uniandes-isis4426/okf/worker/internal/port"
)

// Repo implementa port.JobRepository sobre un pool de conexiones pgx.
type Repo struct {
	pool *pgxpool.Pool
}

var _ port.JobRepository = (*Repo)(nil)

// New abre el pool y verifica la conectividad con un Ping (fallar aquí, al arrancar, es
// mejor que fallar en el primer trabajo).
//
// Ojo con el pool: n_réplicas × maxConns debe quedar por debajo del max_connections de
// Postgres. 20 réplicas con 10 conexiones cada una revientan un Postgres por defecto.
func New(ctx context.Context, databaseURL string, maxConns int32) (*Repo, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL inválida: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("abriendo pool de Postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("Postgres no responde: %w", err)
	}
	return &Repo{pool: pool}, nil
}

// Close cierra el pool.
func (r *Repo) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

// claimSQL toma el trabajo en exclusiva. Las dos ramas del WHERE son el corazón del
// diseño:
//   - status IN ('queued','failed')  → IDEMPOTENCIA: si la cola entrega el mensaje dos
//     veces, el segundo worker encuentra cero filas y se retira.
//   - lease vencido en 'processing'   → RECUPERACIÓN: un worker que murió a mitad dejó
//     el trabajo en 'processing'; al vencer su lease, otro lo re-toma.
//
// La condición viaja DENTRO del WHERE, nunca se comprueba en Go: comprobarlo fuera y
// actualizar después sería una carrera y dos workers pasarían el if.
const claimSQL = `
WITH claimed AS (
    UPDATE jobs
       SET status       = 'processing',
           started_at   = now(),
           heartbeat_at = now(),
           attempts     = attempts + 1
     WHERE id = $1
       AND ( status IN ('queued','failed')
             OR (status = 'processing'
                 AND heartbeat_at < now() - make_interval(secs => $2::int)) )
    RETURNING id, document_id, owner_id, attempts, max_attempts, started_at
)
SELECT c.id, c.owner_id, c.attempts, c.max_attempts, c.started_at,
       d.id, d.filename, d.mime, d.size_bytes, d.storage_key
  FROM claimed c
  JOIN documents d ON d.id = c.document_id`

func (r *Repo) Claim(ctx context.Context, jobID uuid.UUID, lease time.Duration) (domain.ClaimedJob, error) {
	leaseSecs := int(lease.Seconds())
	if leaseSecs < 1 {
		leaseSecs = 1
	}

	var cj domain.ClaimedJob
	err := r.pool.QueryRow(ctx, claimSQL, jobID, leaseSecs).Scan(
		&cj.ID, &cj.OwnerID, &cj.Attempts, &cj.MaxAttempts, &cj.ClaimedAt,
		&cj.Document.ID, &cj.Document.Filename, &cj.Document.MIME,
		&cj.Document.SizeBytes, &cj.Document.StorageKey,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Cero filas: no se pudo tomar. El centinela cubre los tres casos a la vez.
		return domain.ClaimedJob{}, fmt.Errorf("%w: job %s", job.ErrNotClaimable, jobID)
	}
	if err != nil {
		return domain.ClaimedJob{}, fmt.Errorf("tomando el trabajo %s: %w", jobID, err)
	}
	return cj, nil
}

// Heartbeat renueva el lease y hace de canal de cancelación al mismo tiempo. Cero
// filas ⇒ el trabajo ya no está en 'processing' (cancelado o terminado) ⇒ ErrNotClaimable.
func (r *Repo) Heartbeat(ctx context.Context, jobID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE jobs SET heartbeat_at = now() WHERE id = $1 AND status = 'processing'`, jobID)
	if err != nil {
		return fmt.Errorf("renovando lease de %s: %w", jobID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: job %s ya no está en processing", job.ErrNotClaimable, jobID)
	}
	return nil
}

const insertBundleSQL = `
INSERT INTO bundles (id, job_id, owner_id, storage_prefix, validation,
                     warnings, concept_count, size_bytes, published_at)
VALUES ($1, $2, $3, $4, $5::validation_status, $6::jsonb, $7, $8, now())
ON CONFLICT (job_id) DO NOTHING
RETURNING id`

// Publish inserta el bundle y marca el trabajo como succeeded en UNA sola transacción.
// El COMMIT es la publicación: sin fila en bundles, la descarga responde 404.
//
// El ON CONFLICT (job_id) DO NOTHING + el UNIQUE de la tabla garantizan a lo sumo un
// bundle por trabajo aunque dos workers llegaran a la vez; el que pierde recibe
// job.ErrAlreadyPublished (no es un fallo: el receptor hace ack).
func (r *Repo) Publish(ctx context.Context, b domain.Bundle) error {
	warnings, err := json.Marshal(b.Warnings)
	if err != nil {
		return fmt.Errorf("serializando warnings: %w", err)
	}
	if b.Warnings == nil {
		warnings = []byte("[]")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("abriendo transacción: %w", err)
	}
	defer tx.Rollback(ctx) // no-op si ya se hizo Commit

	var insertedID uuid.UUID
	err = tx.QueryRow(ctx, insertBundleSQL,
		b.ID, b.JobID, b.OwnerID, b.StoragePrefix, string(b.Validation),
		string(warnings), b.ConceptCount, b.SizeBytes,
	).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT no insertó: otro worker ya publicó este bundle.
		return job.ErrAlreadyPublished
	}
	if err != nil {
		return fmt.Errorf("insertando bundle de %s: %w", b.JobID, err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE jobs SET status='succeeded', finished_at=now(), error=NULL WHERE id=$1`,
		b.JobID); err != nil {
		return fmt.Errorf("marcando succeeded %s: %w", b.JobID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit de la publicación de %s: %w", b.JobID, err)
	}
	return nil
}

// MarkFailed deja el trabajo reintentable: vuelve a 'failed', que Claim acepta.
func (r *Repo) MarkFailed(ctx context.Context, jobID uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status='failed', error=$2 WHERE id=$1`, jobID, reason)
	if err != nil {
		return fmt.Errorf("marcando failed %s: %w", jobID, err)
	}
	return nil
}

// MarkDead es terminal: agotó max_attempts y no se reintenta más.
func (r *Repo) MarkDead(ctx context.Context, jobID uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status='dead', error=$2, finished_at=now() WHERE id=$1`, jobID, reason)
	if err != nil {
		return fmt.Errorf("marcando dead %s: %w", jobID, err)
	}
	return nil
}
