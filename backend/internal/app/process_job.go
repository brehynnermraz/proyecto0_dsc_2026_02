package app

import (
	"bytes"
	"context"
	"fmt"

	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

// ProcessJobService es el caso de uso que el Worker Consumer invoca por cada
// mensaje de la cola. Es seguro llamarlo dos veces con el mismo jobID:
// ClaimForProcessing solo tiene éxito una vez, así que una entrega duplicada
// no produce un segundo bundle.
type ProcessJobService struct {
	Jobs       ports.JobRepository
	Documents  ports.DocumentRepository
	Bundles    ports.BundleRepository
	Storage    ports.ObjectStore
	Converters []ports.DocumentConverter
}

func (s *ProcessJobService) Process(ctx context.Context, jobID string) error {
	claimed, err := s.Jobs.ClaimForProcessing(ctx, jobID)
	if err != nil {
		return fmt.Errorf("claim job: %w", err)
	}
	if !claimed {
		return nil // ya procesado o en curso por otra entrega: no-op
	}

	job, err := s.Jobs.FindByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("load job: %w", err)
	}

	doc, err := s.Documents.FindByID(ctx, job.DocumentID)
	if err != nil {
		return s.fail(ctx, job, fmt.Errorf("load document: %w", err))
	}

	original, err := s.Storage.GetOriginal(ctx, doc.StorageKey)
	if err != nil {
		return s.fail(ctx, job, fmt.Errorf("fetch original: %w", err))
	}
	defer original.Close()

	converter := s.converterFor(doc.Format)
	if converter == nil {
		return s.fail(ctx, job, fmt.Errorf("formato no soportado: %q", doc.Format))
	}

	units, err := converter.Convert(ctx, original)
	if err != nil {
		return s.fail(ctx, job, fmt.Errorf("convert: %w", err))
	}

	bundle := domain.BuildBundle(job.ID, job.OwnerID, units)
	if !bundle.Valid {
		return s.fail(ctx, job, fmt.Errorf("bundle inválido: %v", bundle.Warnings))
	}

	bundle.StorageKey = fmt.Sprintf("bundles/%s/%s", job.OwnerID, bundle.ID)
	if err := s.writeBundleFiles(ctx, bundle); err != nil {
		return s.fail(ctx, job, fmt.Errorf("write bundle: %w", err))
	}

	if err := s.Bundles.Create(ctx, bundle); err != nil {
		return s.fail(ctx, job, fmt.Errorf("save bundle metadata: %w", err))
	}

	if err := s.Jobs.MarkDone(ctx, job.ID, bundle.ID); err != nil {
		return fmt.Errorf("mark job done: %w", err)
	}
	return nil
}

func (s *ProcessJobService) converterFor(format string) ports.DocumentConverter {
	for _, c := range s.Converters {
		if c.Supports(format) {
			return c
		}
	}
	return nil
}

func (s *ProcessJobService) writeBundleFiles(ctx context.Context, b *domain.Bundle) error {
	index := b.IndexMarkdown()
	if err := s.Storage.PutBundleFile(ctx, b.StorageKey, "index.md", bytes.NewReader([]byte(index)), int64(len(index))); err != nil {
		return err
	}

	logMd := b.LogMarkdown()
	if err := s.Storage.PutBundleFile(ctx, b.StorageKey, "log.md", bytes.NewReader([]byte(logMd)), int64(len(logMd))); err != nil {
		return err
	}

	for _, c := range b.Concepts {
		if err := s.Storage.PutBundleFile(ctx, b.StorageKey, c.Filename, bytes.NewReader([]byte(c.Content)), int64(len(c.Content))); err != nil {
			return err
		}
	}
	return nil
}

// fail dejar registrado el motivo de la falla en el job (MarkFailed) y
// devuelve el error tal cual: es el Worker Consumer quien decide, con ese
// error, hacer Nack y dejar que RabbitMQ enrute a jobs.retry / jobs.dead.
func (s *ProcessJobService) fail(ctx context.Context, job *domain.Job, cause error) error {
	_ = s.Jobs.MarkFailed(ctx, job.ID, job.Attempts+1, cause.Error())
	return cause
}
