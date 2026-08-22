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

const (
	mainQueue  = "jobs"
	retryQueue = "jobs.retry"
	deadQueue  = "jobs.dead"
	exchange   = "jobs.direct"
	retryTTL   = 30 * time.Second // backoff antes de reintentar
)

// Topology declara las tres colas del diagrama de arquitectura:
//
//	jobs        -> cola principal, consumida por los workers
//	jobs.retry  -> cola de espera con TTL; al expirar, el mensaje vuelve a jobs
//	jobs.dead   -> destino final tras superar max_attempts (ver adapters/worker)
//
// Tanto la API (para publicar) como el worker (para consumir) llaman a esta
// función al arrancar, así que la topología queda declarada sin importar
// cuál de los dos procesos arranca primero.
func Topology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(exchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}

	if _, err := ch.QueueDeclare(mainQueue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    exchange,
		"x-dead-letter-routing-key": retryQueue,
	}); err != nil {
		return fmt.Errorf("declare %s: %w", mainQueue, err)
	}
	if err := ch.QueueBind(mainQueue, mainQueue, exchange, false, nil); err != nil {
		return fmt.Errorf("bind %s: %w", mainQueue, err)
	}

	if _, err := ch.QueueDeclare(retryQueue, true, false, false, false, amqp.Table{
		"x-message-ttl":             int32(retryTTL.Milliseconds()),
		"x-dead-letter-exchange":    exchange,
		"x-dead-letter-routing-key": mainQueue,
	}); err != nil {
		return fmt.Errorf("declare %s: %w", retryQueue, err)
	}
	if err := ch.QueueBind(retryQueue, retryQueue, exchange, false, nil); err != nil {
		return fmt.Errorf("bind %s: %w", retryQueue, err)
	}

	if _, err := ch.QueueDeclare(deadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s: %w", deadQueue, err)
	}
	if err := ch.QueueBind(deadQueue, deadQueue, exchange, false, nil); err != nil {
		return fmt.Errorf("bind %s: %w", deadQueue, err)
	}

	return nil
}

// DialWithRetry conecta a RabbitMQ reintentando con backoff fijo. Existe
// porque un healthcheck de "el broker responde" (rabbitmq-diagnostics ping)
// puede pasar un instante antes de que el listener AMQP acepte conexiones:
// sin esto, api y worker pueden arrancar justo en esa ventana y morir con
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

func (p *Publisher) Publish(ctx context.Context, msg ports.QueueMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return p.ch.PublishWithContext(ctx, exchange, mainQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

// RouteToDead se usa cuando un job supera max_attempts: en vez de dejar que
// el TTL de jobs.retry lo reintente indefinidamente, se publica directo a
// jobs.dead para inspección manual.
func RouteToDead(ctx context.Context, ch *amqp.Channel, msg ports.QueueMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return ch.PublishWithContext(ctx, exchange, deadQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}
