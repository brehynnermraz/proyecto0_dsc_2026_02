// Package port define los contratos (interfaces) que el núcleo del worker necesita.
// La regla de dependencia apunta siempre hacia dentro: el núcleo conoce estos
// puertos, no las implementaciones concretas (amqp, postgres, minio...).
//
// port importa domain y nada más; los adaptadores importan port, nunca al revés.
package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
)

// JobProcessor es el puerto primario: el receptor de RabbitMQ recibe una entrega, saca
// el job_id y llama a Process. El error que devuelve decide ack o nack, así que debe
// envolver los centinelas de internal/job.
//
// La firma vuelve a ser solo el job_id. Todo lo demás (filename, mime,
// storage_key) lo relee el procesador de la base de datos al TOMAR el trabajo, porque
// la cola no es la fuente de verdad. Lo implementa job.Processor.
type JobProcessor interface {
	Process(ctx context.Context, jobID uuid.UUID) error
}

// JobRepository es el puerto de la base de datos. Postgres es la fuente de verdad del
// estado; la cola solo entrega trabajo.
type JobRepository interface {
	// Claim toma el trabajo en exclusiva mediante un UPDATE condicional. Devuelve
	// job.ErrNotClaimable si no hay filas (reentrega duplicada, ya terminado o
	// cancelado). lease define cuándo se considera muerto el worker que lo tenía.
	Claim(ctx context.Context, jobID uuid.UUID, lease time.Duration) (domain.ClaimedJob, error)

	// Heartbeat renueva el lease. Devuelve job.ErrNotClaimable si el trabajo dejó de
	// estar en 'processing': ese es el canal de cancelación.
	Heartbeat(ctx context.Context, jobID uuid.UUID) error

	// Publish inserta la fila de bundles y marca el trabajo como succeeded en UNA sola
	// transacción. El COMMIT es la publicación. Devuelve job.ErrAlreadyPublished si
	// otro worker se adelantó.
	Publish(ctx context.Context, b domain.Bundle) error

	// MarkFailed deja el trabajo en 'failed' para que Claim pueda reintentarlo.
	MarkFailed(ctx context.Context, jobID uuid.UUID, reason string) error

	// MarkDead deja el trabajo en 'dead': agotó max_attempts, no se reintenta más.
	MarkDead(ctx context.Context, jobID uuid.UUID, reason string) error
}

// Notifier es el puerto de la notificación best-effort hacia la API. No devuelve error
// a propósito: un fallo aquí nunca debe impedir el ack ni alterar el estado.
type Notifier interface {
	JobChanged(ctx context.Context, change domain.JobChange)
}
