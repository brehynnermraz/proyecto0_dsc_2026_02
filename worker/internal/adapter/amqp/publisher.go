package amqp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/uniandes-isis4426/okf/worker/internal/config"
)

// Publisher publica trabajos en la cola. En producción esto vive en la API; aquí
// existe solo para cmd/seed, que permite desarrollar y probar el worker de punta a
// punta sin una sola línea de la otra aplicación.
type Publisher struct {
	cfg  config.Config
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewPublisher abre conexión, declara la topología y deja el canal listo para publicar.
func NewPublisher(cfg config.Config) (*Publisher, error) {
	conn, err := amqp.Dial(cfg.AMQPURL)
	if err != nil {
		return nil, fmt.Errorf("conectando con el broker: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("abriendo canal: %w", err)
	}
	if err := Declare(ch, cfg); err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}
	return &Publisher{cfg: cfg, conn: conn, ch: ch}, nil
}

// Close cierra canal y conexión.
func (p *Publisher) Close() error {
	if p.ch != nil {
		p.ch.Close()
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

// Publish envía un trabajo a la cola de conversión.
//
// DeliveryMode persistente + cola quorum: el mensaje sobrevive al reinicio del
// broker. Sin eso, apagar RabbitMQ perdería toda la cola.
func (p *Publisher) Publish(ctx context.Context, msg JobMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return p.ch.PublishWithContext(ctx, p.cfg.Exchange, p.cfg.RoutingKey, false, false,
		amqp.Publishing{
			MessageId:    msg.JobID.String(), // clave de deduplicación e idempotencia
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
		})
}
