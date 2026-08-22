// Package amqp es el adaptador primario: recibe mensajes de RabbitMQ y llama al
// puerto JobProcessor. Es el único lugar del worker que conoce amqp091.
package amqp

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/uniandes-isis4426/okf/worker/internal/config"
)

// Nombres de la topología. Los declara el worker al arrancar; declarar algo que ya
// existe con los mismos argumentos no falla, así que es seguro hacerlo en cada réplica.
const (
	ExchangeJobs = "okf.jobs"     // trabajo nuevo
	ExchangeDLX  = "okf.jobs.dlx" // dead-letter: espera y muerte

	QueueConvert = "jobs.convert" // la cola de trabajo
	QueueRetry   = "jobs.retry"   // sala de espera: sin consumidores, con TTL
	QueueDead    = "jobs.dead"    // terminal: inspección manual

	KeyConvert = "convert"
	KeyRetry   = "retry"
	KeyDead    = "dead"
)

// Declare crea exchanges, colas y bindings. Es idempotente.
//
// El backoff sale gratis y sin código propio: un Nack(requeue=false) en
// jobs.convert manda el mensaje al DLX con la routing key "retry", jobs.retry no
// tiene consumidores, y al expirar su TTL el broker lo devuelve solo a
// jobs.convert. Nadie duerme, nadie reencola a mano.
//
//	jobs.convert ──Nack──▶ okf.jobs.dlx ──"retry"──▶ jobs.retry
//	     ▲                                              │ TTL 30 s
//	     └───────────── okf.jobs ──"convert"────────────┘
func Declare(ch *amqp.Channel, cfg config.Config) error {
	for _, ex := range []string{ExchangeJobs, ExchangeDLX} {
		if err := ch.ExchangeDeclare(ex, "direct", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declarando exchange %s: %w", ex, err)
		}
	}

	// Cola de trabajo. Quorum para que sobreviva al reinicio de un nodo del broker;
	// x-delivery-limit es la red de seguridad ante un bucle de reentrega.
	if _, err := ch.QueueDeclare(QueueConvert, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    ExchangeDLX,
		"x-dead-letter-routing-key": KeyRetry,
		"x-delivery-limit":          int32(cfg.DeliveryLimit),
	}); err != nil {
		return fmt.Errorf("declarando cola %s: %w", QueueConvert, err)
	}
	if err := ch.QueueBind(QueueConvert, KeyConvert, ExchangeJobs, false, nil); err != nil {
		return fmt.Errorf("enlazando %s: %w", QueueConvert, err)
	}

	// Sala de espera. Sin consumidores a propósito: el TTL es el reloj del backoff.
	if _, err := ch.QueueDeclare(QueueRetry, true, false, false, false, amqp.Table{
		"x-message-ttl":             int32(cfg.RetryTTL.Milliseconds()),
		"x-dead-letter-exchange":    ExchangeJobs,
		"x-dead-letter-routing-key": KeyConvert,
	}); err != nil {
		return fmt.Errorf("declarando cola %s: %w", QueueRetry, err)
	}
	if err := ch.QueueBind(QueueRetry, KeyRetry, ExchangeDLX, false, nil); err != nil {
		return fmt.Errorf("enlazando %s: %w", QueueRetry, err)
	}

	// Terminal.  cuando un trabajo agote max_attempts, se publicará
	// aquí explícitamente antes de hacer ack. Por ahora solo se declara.
	if _, err := ch.QueueDeclare(QueueDead, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declarando cola %s: %w", QueueDead, err)
	}
	if err := ch.QueueBind(QueueDead, KeyDead, ExchangeDLX, false, nil); err != nil {
		return fmt.Errorf("enlazando %s: %w", QueueDead, err)
	}

	return nil
}
