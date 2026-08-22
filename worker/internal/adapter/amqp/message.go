package amqp

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// JobMessage es el contrato del mensaje que la API publica y el worker consume.
// Es uno de los dos únicos puntos de contacto entre las dos aplicaciones (el otro
// es el esquema de migrations/)
//
//	{"job_id":"0192...","document_id":"0192...","owner_id":"0192...","mime":"text/markdown"}
//
// El campo que manda es job_id. Los demás son informativos: el
// worker los relee de la base de datos al tomar el trabajo, porque la cola no es la
// fuente de verdad.
//
// El mensaje se reduce prácticamente a job_id. La storage_key, el mime y el
// filename ya NO viajan por la cola: el worker los relee de la base de datos al TOMAR
// el trabajo (Claim), porque la cola no es la fuente de verdad. Los campos que quedan
// son informativos (para logs/depuración en la UI del broker).
type JobMessage struct {
	JobID      uuid.UUID `json:"job_id"`
	DocumentID uuid.UUID `json:"document_id,omitempty"`
	OwnerID    uuid.UUID `json:"owner_id,omitempty"`
}

// ErrMalformed marca un mensaje que nunca podrá procesarse: sin job_id utilizable.
// Reintentarlo no cambiaría nada, así que se registra y se confirma (ack).
var ErrMalformed = errors.New("mensaje mal formado")

// ParseDelivery extrae el job_id de una entrega.
//
// Se prefiere la propiedad MessageId de AMQP porque es también la clave que usa el
// broker para deduplicar y la que aparece en la UI de gestión, lo que hace que
// depurar sea mucho más fácil. El cuerpo JSON es el respaldo.
func ParseDelivery(d amqp.Delivery) (JobMessage, error) {
	var msg JobMessage

	if len(d.Body) > 0 {
		// Un cuerpo ilegible no es fatal mientras MessageId sirva.
		_ = json.Unmarshal(d.Body, &msg)
	}

	if d.MessageId != "" {
		id, err := uuid.Parse(d.MessageId)
		if err != nil {
			return msg, fmt.Errorf("%w: MessageId %q no es un UUID: %v", ErrMalformed, d.MessageId, err)
		}
		msg.JobID = id
		return msg, nil
	}

	if msg.JobID == uuid.Nil {
		return msg, fmt.Errorf("%w: sin MessageId y sin job_id en el cuerpo", ErrMalformed)
	}
	return msg, nil
}
