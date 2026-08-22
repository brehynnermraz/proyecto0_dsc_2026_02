package amqp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/uniandes-isis4426/okf/worker/internal/config"
	"github.com/uniandes-isis4426/okf/worker/internal/job"
	"github.com/uniandes-isis4426/okf/worker/internal/port"
)

// Consumer es el adaptador primario: traduce entregas de AMQP en llamadas al
// procesador y traduce el error que este devuelve en ack o nack.
type Consumer struct {
	cfg       config.Config
	processor port.JobProcessor
	log       *slog.Logger

	// inFlight cuenta los trabajos en curso para que el apagado ordenado espere.
	inFlight sync.WaitGroup

	// alive indica si hay conexión viva con el broker; lo consulta /readyz.
	mu    sync.RWMutex
	alive bool
}

// NewConsumer arma el receptor con su procesador.
func NewConsumer(cfg config.Config, p port.JobProcessor, log *slog.Logger) *Consumer {
	return &Consumer{cfg: cfg, processor: p, log: log}
}

// Alive indica si hay una conexión viva con el broker.
func (c *Consumer) Alive() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.alive
}

func (c *Consumer) setAlive(v bool) {
	c.mu.Lock()
	c.alive = v
	c.mu.Unlock()
}

// Run mantiene la conexión con el broker hasta que se cancele el contexto.
//
// amqp091-go NO reconecta solo: si el broker se reinicia, la conexión queda muerta
// y el worker se convierte en un zombi silencioso. Por eso este bucle existe desde
// el primer día y no como pulido final.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		err := c.session(ctx)

		if ctx.Err() != nil {
			c.log.Info("receptor detenido por apagado ordenado")
			return nil
		}

		c.setAlive(false)
		c.log.Error("sesión AMQP terminada, reintentando",
			"err", err, "delay", c.cfg.ReconnectDelay)

		select {
		case <-time.After(c.cfg.ReconnectDelay):
		case <-ctx.Done():
			return nil
		}
	}
}

// session abre conexión y canal, declara la topología y consume hasta que algo falle.
func (c *Consumer) session(ctx context.Context) error {
	conn, err := amqp.Dial(c.cfg.AMQPURL)
	if err != nil {
		return fmt.Errorf("conectando con el broker: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abriendo canal: %w", err)
	}
	defer ch.Close()

	if err := Declare(ch, c.cfg); err != nil {
		return fmt.Errorf("declarando topología: %w", err)
	}

	c.log.Info("conectado al broker", "queue", c.cfg.Queue, "prefetch", c.cfg.Prefetch)
	return c.consume(ctx, ch)
}

// consume es el bucle de entregas.
func (c *Consumer) consume(ctx context.Context, ch *amqp.Channel) error {
	// prefetch = 1 no es afinación fina: es cuántos mensajes sin confirmar deja el
	// broker en la mano. Con 1, el reparto deja de ser round-robin por cantidad y
	// pasa a ser reparto por capacidad real — al worker que le tocó un documento
	// enorme no le llega más trabajo mientras lo mastica.
	if err := ch.Qos(c.cfg.Prefetch, 0, false); err != nil {
		return fmt.Errorf("fijando QoS: %w", err)
	}

	deliveries, err := ch.Consume(
		c.cfg.Queue,
		c.cfg.ConsumerTag,
		false, // autoAck = false: la confirmación es manual (a partir del paso 2, tras el COMMIT)
		false, // exclusive: nunca, aquí competimos con las demás réplicas
		false, // noLocal
		false, // noWait
		nil,
	)
	if err != nil {
		return fmt.Errorf("suscribiéndose a %s: %w", c.cfg.Queue, err)
	}

	closed := ch.NotifyClose(make(chan *amqp.Error, 1))
	c.setAlive(true)

	for {
		select {
		case <-ctx.Done():
			// SIGTERM: dejar de tomar trabajo nuevo. El trabajo en curso lo espera
			// Wait() desde el apagado ordenado.
			return nil

		case reason := <-closed:
			if reason == nil {
				return errors.New("canal cerrado sin motivo")
			}
			return fmt.Errorf("canal cerrado: %w", reason)

		case d, ok := <-deliveries:
			if !ok {
				return errors.New("canal de entregas cerrado")
			}
			c.handle(ctx, d)
		}
	}
}

// handle procesa una entrega y decide su destino.
//
// Se ejecuta en serie a propósito: con prefetch=1 hay como mucho un mensaje sin
// confirmar por réplica, así que un trabajo a la vez por contenedor. La
// concurrencia se consigue con más réplicas, que es lo que evalúa el enunciado.
func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) {
	c.inFlight.Add(1)
	defer c.inFlight.Done()

	msg, err := ParseDelivery(d)
	if err != nil {
		// Nunca podrá procesarse. Reencolarlo sería un bucle eterno.
		c.log.Error("descartando mensaje", "err", err, "delivery_tag", d.DeliveryTag)
		c.ack(d)
		return
	}

	log := c.log.With("job_id", msg.JobID.String(), "redelivered", d.Redelivered)

	// PASO 2: al procesador le basta el job_id. Todo lo demás lo relee de la base de
	// datos al tomar el trabajo (la cola no es la fuente de verdad).
	start := time.Now()
	err = c.processor.Process(ctx, msg.JobID)
	elapsed := time.Since(start)

	switch {
	case err == nil:
		log.Info("trabajo completado", "duration", elapsed)
		c.ack(d)

	case errors.Is(err, job.ErrNotClaimable):
		// No se pudo tomar: reentrega duplicada, ya terminado o cancelado. No es un
		// fallo; se descarta el mensaje.
		log.Info("trabajo no reclamable, se descarta", "err", err, "duration", elapsed)
		c.ack(d)

	case errors.Is(err, job.ErrCancelled):
		// El latido detectó cancelación a mitad. El estado ya lo fijó quien canceló.
		log.Info("trabajo cancelado, se descarta", "err", err, "duration", elapsed)
		c.ack(d)

	case errors.Is(err, job.ErrExhausted):
		// Agotó max_attempts; el procesador ya lo marcó 'dead'.
		log.Warn("trabajo agotado (dead), se descarta", "err", err, "duration", elapsed)
		c.ack(d)

	case errors.Is(err, job.ErrPermanent):
		// Reintentar no cambiaría el resultado; el trabajo quedó 'failed'. El usuario
		// podrá pedir el reintento desde la API.
		log.Warn("fallo permanente, sin reintento automático", "err", err, "duration", elapsed)
		c.ack(d)

	default:
		// Transitorio (incluye ErrTransient): red o servicios caídos.
		// Nack(multiple=false, requeue=false) es lo que manda el mensaje al DLX y de
		// ahí a jobs.retry. requeue=true lo devolvería al frente de la misma cola sin
		// espera y crearía un bucle apretado que satura el broker: nunca se usa.
		log.Error("fallo transitorio, a la sala de espera", "err", err,
			"duration", elapsed, "retry_in", c.cfg.RetryTTL)
		if err := d.Nack(false, false); err != nil {
			log.Error("no se pudo hacer nack", "err", err)
		}
	}
}

func (c *Consumer) ack(d amqp.Delivery) {
	if err := d.Ack(false); err != nil {
		c.log.Error("no se pudo confirmar el mensaje", "err", err, "delivery_tag", d.DeliveryTag)
	}
}

// Wait bloquea hasta que terminen los trabajos en curso o venza el plazo. Un apagado
// brusco no perderá trabajo cuando exista el lease; esperar evita la demora
// en el caso normal de escalar hacia abajo.
func (c *Consumer) Wait(grace time.Duration) {
	done := make(chan struct{})
	go func() {
		c.inFlight.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.log.Info("no quedan trabajos en curso")
	case <-time.After(grace):
		c.log.Warn("plazo de apagado agotado con trabajo en curso", "grace", grace)
	}
}
