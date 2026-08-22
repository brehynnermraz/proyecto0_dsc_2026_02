package ports

import "context"

// QueueMessage es el contrato del mensaje que la API publica y el worker
// (módulo aparte, ../worker) consume. DEBE coincidir con el JobMessage del
// worker (worker/internal/adapter/amqp/message.go):
//
//	{"job_id":"...","document_id":"...","owner_id":"..."}
//
// El que manda es job_id: el worker además lo lee de la propiedad MessageId de
// AMQP (que el Publisher fija), y relee document_id/owner_id de la base de
// datos al tomar el trabajo, porque la cola no es la fuente de verdad. Aquí no
// viaja el número de intento: el reintento lo cuenta el propio worker sobre la
// base de datos (columna jobs.attempts) y la cabecera x-death del broker.
type QueueMessage struct {
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id,omitempty"`
	OwnerID    string `json:"owner_id,omitempty"`
}

// JobQueue es lo único que el dominio conoce de la mensajería: publicar. El
// consumo con ack manual, reintentos y dead-lettering son mecánica del worker
// (ver ../worker) y no pertenecen ni al dominio ni a la API.
type JobQueue interface {
	Publish(ctx context.Context, msg QueueMessage) error
}
