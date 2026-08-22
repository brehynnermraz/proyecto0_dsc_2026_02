package db

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"okfbundler/internal/adapters/events"
)

// ListenJobStatus mantiene una conexión dedicada escuchando el canal
// "job_status" (ver migración 0002_job_status_notify.sql) y publica cada
// notificación en el hub para que los handlers SSE la reciban.
//
// LISTEN/NOTIFY necesita sostener una conexión propia: no se puede hacer
// sobre pgxpool, que reutiliza y libera conexiones por consulta. Si la
// conexión se cae, se reintenta con un backoff fijo.
func ListenJobStatus(ctx context.Context, dsn string, hub *events.Hub) {
	for ctx.Err() == nil {
		if err := listenOnce(ctx, dsn, hub); err != nil && ctx.Err() == nil {
			log.Printf("job status listener: %v (reintentando en 3s)", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func listenOnce(ctx context.Context, dsn string, hub *events.Hub) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "LISTEN job_status"); err != nil {
		return err
	}

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}

		jobID, status, ok := strings.Cut(n.Payload, ":")
		if !ok {
			continue
		}
		hub.Publish(events.JobStatusEvent{JobID: jobID, Status: status})
	}
}
