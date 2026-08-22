package worker

import (
	"context"
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"

	"okfbundler/internal/adapters/queue"
	"okfbundler/internal/app"
	"okfbundler/internal/ports"
)

// maxAttempts: tras superarlo, el mensaje se enruta a jobs.dead en vez de
// reintentarse otra vez vía jobs.retry.
const maxAttempts = 5

type Consumer struct {
	Channel *amqp.Channel
	Process *app.ProcessJobService
}

// Run consume "jobs" con ack manual (consume · ack manual del diagrama). Un
// Process exitoso hace Ack; cualquier error hace Nack(requeue=false), que
// por la topología de Topology() cae en jobs.retry y, tras maxAttempts, en
// jobs.dead.
func (c *Consumer) Run(ctx context.Context) error {
	deliveries, err := c.Channel.Consume("jobs", "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return nil
			}
			c.handle(ctx, d)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) {
	var msg ports.QueueMessage
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		log.Printf("mensaje inválido, se descarta: %v", err)
		d.Nack(false, false) // un JSON corrupto nunca va a procesar; no reintentar
		return
	}

	// RabbitMQ registra cada ciclo de dead-lettering en la cabecera x-death;
	// contarlos es más confiable que llevar el intento en el propio body,
	// porque el body no cambia al pasar por jobs.retry.
	attempt := deathCount(d) + 1

	if attempt > maxAttempts {
		log.Printf("job %s superó max_attempts (%d), va a jobs.dead", msg.JobID, maxAttempts)
		if err := queue.RouteToDead(ctx, c.Channel, msg); err != nil {
			log.Printf("no se pudo enrutar job %s a jobs.dead: %v", msg.JobID, err)
		}
		d.Ack(false)
		return
	}

	if err := c.Process.Process(ctx, msg.JobID); err != nil {
		log.Printf("job %s falló (intento %d/%d): %v", msg.JobID, attempt, maxAttempts, err)
		d.Nack(false, false)
		return
	}

	d.Ack(false)
}

func deathCount(d amqp.Delivery) int {
	xDeath, ok := d.Headers["x-death"].([]interface{})
	if !ok {
		return 0
	}
	return len(xDeath)
}
