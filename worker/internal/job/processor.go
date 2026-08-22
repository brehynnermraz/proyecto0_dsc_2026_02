package job

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/uniandes-isis4426/okf/worker/internal/adapter/objectstore"
	"github.com/uniandes-isis4426/okf/worker/internal/config"
	"github.com/uniandes-isis4426/okf/worker/internal/converter"
	"github.com/uniandes-isis4426/okf/worker/internal/domain"
	"github.com/uniandes-isis4426/okf/worker/internal/port"
)

// SimulatedProcessor implementa port.JobProcessor SIN base de datos ni conversión: fue
// lo único que corrió en el PASO 1 (log + sleep → ack). Se conserva para ejercitar solo
// la mensajería. Ya NO se enchufa en main (el paso 2 usa Processor); vive aquí como
// referencia y para pruebas del receptor.
type SimulatedProcessor struct {
	cfg config.Config
	log *slog.Logger
}

func NewSimulatedProcessor(cfg config.Config, log *slog.Logger) *SimulatedProcessor {
	return &SimulatedProcessor{cfg: cfg, log: log}
}

func (p *SimulatedProcessor) Process(ctx context.Context, jobID uuid.UUID) error {
	p.log.Info("procesando (simulado)", "job_id", jobID, "trabajo", p.cfg.SimulateWork)
	select {
	case <-time.After(p.cfg.SimulateWork):
	case <-ctx.Done():
		return fmt.Errorf("%w: contexto cancelado: %v", ErrTransient, ctx.Err())
	}
	if p.cfg.SimulateFailure {
		return fmt.Errorf("%w: SIMULATE_FAILURE=true", ErrTransient)
	}
	p.log.Info("trabajo simulado completado", "job_id", jobID)
	return nil
}

// Processor es el procesador real: toma el trabajo en Postgres, lee el
// original del object store, lo convierte y publica el bundle transaccionalmente.
//
// Solo conoce puertos: no sabe si detrás del store hay una carpeta o un servicio HTTP,
// ni si la notificación viaja o no. Ese es todo el propósito del hexágono.
//
// El conversor ya es el real (converter.Convert: parse → segmentar → renderizar →
// validar). Lo de este procesador es lo de alrededor: claim, lease/heartbeat, publish
// transaccional y traducción de errores.
type Processor struct {
	repo     port.JobRepository
	store    objectstore.Store
	notifier port.Notifier
	cfg      config.Config
	log      *slog.Logger
}

var _ port.JobProcessor = (*Processor)(nil)

func NewProcessor(repo port.JobRepository, store objectstore.Store, notifier port.Notifier, cfg config.Config, log *slog.Logger) *Processor {
	return &Processor{repo: repo, store: store, notifier: notifier, cfg: cfg, log: log}
}

// Process ejecuta el trabajo completo. El error que devuelve decide el ack/nack, así
// que SIEMPRE envuelve un centinela (o es transitorio → nack).
//
// Dos órdenes que no se pueden invertir:
//   - PRIMERO el store (Put de los archivos), DESPUÉS el COMMIT (Publish). Al revés
//     dejaría filas apuntando a archivos que no existen. Un objeto huérfano, en cambio,
//     es inofensivo: sin fila en bundles nadie lo descarga.
//   - El worker toma el trabajo (Claim) ANTES de tocar nada: la cola solo avisa, la
//     base de datos es la que autoriza.
func (p *Processor) Process(ctx context.Context, jobID uuid.UUID) error {
	// 1. Tomar el trabajo en exclusiva. Cero filas → ErrNotClaimable (ack, no es fallo).
	cj, err := p.repo.Claim(ctx, jobID, p.cfg.LeaseTTL)
	if err != nil {
		return err // ErrNotClaimable (ack) o error de BD (transitorio → nack)
	}
	log := p.log.With("job_id", jobID.String(), "intento", cj.Attempts, "storage_key", cj.Document.StorageKey)

	// 2. ¿Agotó intentos? → dead + ack. attempts ya se incrementó en Claim.
	if cj.Attempts > cj.MaxAttempts {
		reason := fmt.Sprintf("agotó max_attempts (%d/%d)", cj.Attempts, cj.MaxAttempts)
		if derr := p.repo.MarkDead(ctx, jobID, reason); derr != nil {
			return fmt.Errorf("%w: %v", ErrTransient, derr) // no pudo marcar dead → reintentar
		}
		p.notify(ctx, jobID, domain.StatusDead, nil, reason)
		return fmt.Errorf("%w: %s", ErrExhausted, reason)
	}

	// 3. Lanzar el latido: renueva el lease y, si devuelve cero filas (cancelación),
	//    cancela el contexto de trabajo para abortar la conversión en curso.
	var cancelled atomic.Bool
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	workCtx, workCancel := context.WithTimeout(hbCtx, p.cfg.JobTimeout)
	defer workCancel()
	go p.heartbeat(hbCtx, func() { cancelled.Store(true); hbCancel() }, jobID)

	// classify traduce el error de una operación de E/S del pipeline. Si el latido
	// canceló, es ErrCancelled; si venció el JobTimeout o hubo un fallo de red, es
	// transitorio (nack).
	classify := func(what string, err error) error {
		if cancelled.Load() {
			return fmt.Errorf("%w: cancelado durante %s", ErrCancelled, what)
		}
		return fmt.Errorf("%w: %s: %v", ErrTransient, what, err)
	}

	// 4. Leer el original del store, acotado (entrada no confiable). Inexistente →
	//    permanente; otro fallo → transitorio o cancelado.
	raw, err := p.readOriginal(workCtx, cj.Document.StorageKey)
	if err != nil {
		if errors.Is(err, objectstore.ErrNotFound) {
			p.fail(ctx, jobID, err)
			return Permanent(fmt.Errorf("original inexistente %s: %w", cj.Document.StorageKey, err))
		}
		return classify("leyendo el original", err)
	}

	// 5. Convertir: parse → segmentar → renderizar → validar (núcleo puro). Un error
	//    aquí (MIME no soportado, archivo corrupto) es permanente: reintentar no ayuda.
	res, err := converter.Convert(raw, converter.Source{
		JobID:     jobID.String(),
		Filename:  cj.Document.Filename,
		MIME:      cj.Document.MIME,
		SizeBytes: cj.Document.SizeBytes,
		Attempt:   cj.Attempts,
		StartedAt: cj.ClaimedAt,
	})
	if err != nil {
		p.fail(ctx, jobID, err)
		return Permanent(err)
	}

	// 6. Bundle inválido → no se sube ni se publica; queda 'failed' (permanente).
	if res.Validation.Status == domain.ValidationInvalid {
		reason := "bundle inválido: " + strings.Join(res.Validation.Errors, "; ")
		p.fail(ctx, jobID, errors.New(reason))
		return fmt.Errorf("%w: %s", ErrPermanent, reason)
	}

	// 7. Subir cada archivo del bundle al store (ANTES del COMMIT).
	bundleID := uuid.New()
	prefix := fmt.Sprintf("bundles/%s/%s", cj.OwnerID, bundleID)
	var total int64
	for name, body := range res.Files {
		key := prefix + "/" + name
		if err := p.store.Put(workCtx, key, bytes.NewReader(body), int64(len(body)), "text/markdown"); err != nil {
			return classify("guardando "+key, err)
		}
		total += int64(len(body))
	}

	// 8. Publicar: insertar el bundle y marcar succeeded en una transacción.
	b := domain.Bundle{
		ID:            bundleID,
		JobID:         jobID,
		OwnerID:       cj.OwnerID,
		StoragePrefix: prefix,
		Validation:    res.Validation.Status,
		Warnings:      res.Validation.Warnings,
		ConceptCount:  int32(res.Units),
		SizeBytes:     total,
	}
	if err := p.repo.Publish(ctx, b); err != nil {
		if errors.Is(err, ErrAlreadyPublished) {
			log.Info("otro worker ya publicó este bundle, nada que hacer")
			return nil // ack
		}
		return classify("publicando el bundle", err)
	}

	// 9. Notificar a la API (best-effort: nunca impide el ack).
	p.notify(ctx, jobID, domain.StatusSucceeded, &bundleID, "")

	log.Info("bundle publicado",
		"bundle_id", bundleID, "unidades", res.Units, "archivos", len(res.Files),
		"bytes", total, "validation", res.Validation.Status,
		"warnings", len(res.Validation.Warnings), "prefijo", prefix)
	return nil
}

// heartbeat renueva el lease periódicamente. Cero filas actualizadas ⇒ el trabajo dejó
// de estar en 'processing' (cancelado o terminado): se llama a onCancel para abortar.
func (p *Processor) heartbeat(ctx context.Context, onCancel func(), jobID uuid.UUID) {
	t := time.NewTicker(p.cfg.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.repo.Heartbeat(ctx, jobID); err != nil {
				if errors.Is(err, context.Canceled) {
					return // apagado normal, no es cancelación de negocio
				}
				p.log.Info("trabajo cancelado o terminado, abortando la conversión",
					"job_id", jobID.String(), "err", err)
				onCancel()
				return
			}
		}
	}
}

// readOriginal trae el original del store y lo lee acotado a MaxInputSize.
func (p *Processor) readOriginal(ctx context.Context, key string) ([]byte, error) {
	rc, err := p.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, p.cfg.MaxInputSize))
}

// fail deja el trabajo reintentable-desde-la-API (status 'failed') y registra el motivo.
// Se usa en los fallos permanentes: no se reintenta solo, pero queda el rastro del error.
func (p *Processor) fail(ctx context.Context, jobID uuid.UUID, cause error) {
	if err := p.repo.MarkFailed(ctx, jobID, cause.Error()); err != nil {
		p.log.Error("no se pudo marcar el trabajo como failed", "job_id", jobID.String(), "err", err)
	}
	p.notify(ctx, jobID, domain.StatusFailed, nil, cause.Error())
}

func (p *Processor) notify(ctx context.Context, jobID uuid.UUID, status domain.JobStatus, bundleID *uuid.UUID, errMsg string) {
	if p.notifier == nil {
		return
	}
	p.notifier.JobChanged(ctx, domain.JobChange{JobID: jobID, Status: status, BundleID: bundleID, Error: errMsg})
}
