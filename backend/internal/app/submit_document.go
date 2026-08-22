package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

// MIMEForFormat traduce el "format" que envía el frontend al MIME que el worker
// (../worker) usa para elegir el parser. SOLO se ofrecen los formatos que el
// worker realmente soporta hoy (ver worker/internal/converter/parser/registry.go):
// HTML y EPUB. Markdown, texto plano, docx y pdf están pendientes allá
// (PENDIENTES.md), así que aquí no se aceptan: mejor un 400 al subir que un job
// que falla permanentemente después.
func MIMEForFormat(format string) (string, bool) {
	switch format {
	case "html":
		return "text/html", true
	case "epub":
		return "application/epub+zip", true
	default:
		return "", false
	}
}

// SubmitDocumentService es el caso de uso detrás de POST /documents: guarda el
// original en el object store, registra el documento y el job, y publica el
// mensaje en la cola. No espera a que el worker procese — la API responde de
// inmediato (sección 6, "asincronía efectiva").
type SubmitDocumentService struct {
	Documents ports.DocumentRepository
	Jobs      ports.JobRepository
	Storage   ports.ObjectStore
	Queue     ports.JobQueue
}

// Submit recibe ya el MIME resuelto (el handler valida el format contra
// MIMEForFormat). storageKey sigue la convención compartida con el worker:
// originals/{owner_id}/{document_id}.
func (s *SubmitDocumentService) Submit(ctx context.Context, ownerID, filename, mime string, size int64, body io.Reader) (jobID string, err error) {
	docID := uuid.NewString()
	storageKey := fmt.Sprintf("originals/%s/%s", ownerID, docID)

	if err := s.Storage.PutOriginal(ctx, storageKey, body, size, mime); err != nil {
		return "", fmt.Errorf("store original: %w", err)
	}

	doc := &domain.Document{
		ID:         docID,
		OwnerID:    ownerID,
		Filename:   filename,
		MIME:       mime,
		StorageKey: storageKey,
		SizeBytes:  size,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.Documents.Create(ctx, doc); err != nil {
		return "", fmt.Errorf("save document metadata: %w", err)
	}

	// El status ('queued'), attempts y max_attempts los pone el default del
	// esquema del worker; aquí solo se ata el job a su documento y dueño.
	job := &domain.Job{
		ID:         uuid.NewString(),
		DocumentID: doc.ID,
		OwnerID:    ownerID,
		Status:     domain.JobQueued,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.Jobs.Create(ctx, job); err != nil {
		return "", fmt.Errorf("save job: %w", err)
	}

	if err := s.Queue.Publish(ctx, ports.QueueMessage{
		JobID:      job.ID,
		DocumentID: doc.ID,
		OwnerID:    ownerID,
	}); err != nil {
		return "", fmt.Errorf("publish job: %w", err)
	}

	return job.ID, nil
}
