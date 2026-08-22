package ports

import "context"

type QueueMessage struct {
	JobID   string `json:"job_id"`
	Attempt int    `json:"attempt"`
}

// JobQueue es lo único que el dominio conoce de la mensajería: publicar. El
// consumo con ack manual, reintentos y dead-lettering son mecánica de
// adaptador (ver internal/adapters/worker y internal/adapters/queue) y no
// pertenecen al dominio.
type JobQueue interface {
	Publish(ctx context.Context, msg QueueMessage) error
}
