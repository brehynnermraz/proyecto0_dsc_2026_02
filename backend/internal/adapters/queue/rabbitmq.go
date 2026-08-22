package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"okfbundler/internal/ports"
)

// Nombres y argumentos de la topología. DEBEN COINCIDIR EXACTAMENTE con los del
// worker (worker/internal/adapter/amqp/topology.go): declarar una cola o un
// exchange que ya existe con argumentos DISTINTOS hace fallar el canal
// ("inequivalent args"). La API y el worker declaran la misma topología, cada
// uno al arrancar, y como es idempotente no importa cuál lo haga primero.
//
// La API solo publica en okf.jobs con la routing key "convert"; el resto de la
// mecánica (reintentos vía jobs.retry, muerte en jobs.dead) es cosa del worker.
const (
	exchangeJobs = "okf.jobs"     // trabajo nuevo
	exchangeDLX  = "okf.jobs.dlx" // dead-letter: espera y muerte

	queueConvert = "jobs.convert" // cola de trabajo (la consume el worker)
	queueRetry   = "jobs.retry"   // sala de espera: sin consumidores, con TTL
	queueDead    = "jobs.dead"    // terminal: inspección manual

	keyConvert = "convert"
	keyRetry   = "retry"
	keyDead    = "dead"

	// Deben igualar los valores por defecto del worker (config.Config):
	// AMQP_DELIVERY_LIMIT=20, AMQP_RETRY_TTL=30s.
	deliveryLimit = int32(20)
	retryTTL      = 30 * time.Second
)

// Topology declara exchanges, colas y bindings tal como los espera el worker.
// Es idempotente. El backoff sale gratis: un Nack(requeue=false) en
// jobs.convert manda el mensaje al DLX con la key "retry"; jobs.retry no tiene
// consumidores y al expirar su TTL el broker lo devuelve solo a jobs.convert.
//
//	jobs.convert ──Nack──▶ okf.jobs.dlx ──"retry"──▶ jobs.retry
//	     ▲                                              │ TTL 30 s
//	     └───────────── okf.jobs ──"convert"────────────┘
func Topology(ch *amqp.Channel) error {
	for _, ex := range []string{exchangeJobs, exchangeDLX} {
		if err := ch.ExchangeDeclare(ex, "direct", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare exchange %s: %w", ex, err)
		}
	}

	// Cola de trabajo. Quorum + x-delivery-limit exactamente como el worker.
	if _, err := ch.QueueDeclare(queueConvert, true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    exchangeDLX,
		"x-dead-letter-routing-key": keyRetry,
		"x-delivery-limit":          deliveryLimit,
	}); err != nil {
		return fmt.Errorf("declare %s: %w", queueConvert, err)
	}
	if err := ch.QueueBind(queueConvert, keyConvert, exchangeJobs, false, nil); err != nil {
		return fmt.Errorf("bind %s: %w", queueConvert, err)
	}

	// Sala de espera: el TTL es el reloj del backoff.
	if _, err := ch.QueueDeclare(queueRetry, true, false, false, false, amqp.Table{
		"x-message-ttl":             int32(retryTTL.Milliseconds()),
		"x-dead-letter-exchange":    exchangeJobs,
		"x-dead-letter-routing-key": keyConvert,
	}); err != nil {
		return fmt.Errorf("declare %s: %w", queueRetry, err)
	}
	if err := ch.QueueBind(queueRetry, keyRetry, exchangeDLX, false, nil); err != nil {
		return fmt.Errorf("bind %s: %w", queueRetry, err)
	}

	// Terminal.
	if _, err := ch.QueueDeclare(queueDead, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", queueDead, err)
	}
	if err := ch.QueueBind(queueDead, keyDead, exchangeDLX, false, nil); err != nil {
		return fmt.Errorf("bind %s: %w", queueDead, err)
	}

	return nil
}

// DialWithRetry conecta a RabbitMQ reintentando con backoff fijo. Existe
// porque un healthcheck de "el broker responde" (rabbitmq-diagnostics ping)
// puede pasar un instante antes de que el listener AMQP acepte conexiones:
// sin esto, la API puede arrancar justo en esa ventana y morir con
// "connection refused" aunque Docker Compose ya marcó rabbitmq como healthy.
func DialWithRetry(url string, attempts int, delay time.Duration) (*amqp.Connection, error) {
	var lastErr error
	for i := 1; i <= attempts; i++ {
		conn, err := amqp.Dial(url)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		log.Printf("rabbitmq: intento %d/%d falló: %v", i, attempts, err)
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("rabbitmq: agotados %d intentos: %w", attempts, lastErr)
}

type Publisher struct {
	ch *amqp.Channel
}

func NewPublisher(ch *amqp.Channel) *Publisher {
	return &Publisher{ch: ch}
}

var _ ports.JobQueue = (*Publisher)(nil)

// Publish encola el trabajo para el worker. Fija MessageId = job_id porque el
// worker lo prefiere sobre el cuerpo (es también la clave de deduplicación del
// broker y lo que se ve en la UI de gestión), y publica en okf.jobs con la key
// "convert", que es donde el worker tiene atada su cola jobs.convert.
func (p *Publisher) Publish(ctx context.Context, msg ports.QueueMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return p.ch.PublishWithContext(ctx, exchangeJobs, keyConvert, false, false, amqp.Publishing{
		MessageId:    msg.JobID,
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}
