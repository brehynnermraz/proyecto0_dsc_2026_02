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

// SubmitDocumentService es el caso de uso detrás de POST /documents: guarda
// el original, registra el trabajo y lo publica en la cola. No espera a que
// el worker lo procese — por eso la API responde de inmediato (sección 6,
// "asincronía efectiva").
type SubmitDocumentService struct {
	Documents ports.DocumentRepository
	Jobs      ports.JobRepository
	Storage   ports.ObjectStore
	Queue     ports.JobQueue
}

func (s *SubmitDocumentService) Submit(ctx context.Context, ownerID, filename, format string, size int64, contentType string, body io.Reader) (jobID string, err error) {
	docID := uuid.NewString()
	storageKey := fmt.Sprintf("originals/%s/%s", ownerID, docID)

	if err := s.Storage.PutOriginal(ctx, storageKey, body, size, contentType); err != nil {
		return "", fmt.Errorf("store original: %w", err)
	}

	doc := &domain.Document{
		ID:         docID,
		OwnerID:    ownerID,
		Filename:   filename,
		Format:     format,
		StorageKey: storageKey,
		SizeBytes:  size,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.Documents.Create(ctx, doc); err != nil {
		return "", fmt.Errorf("save document metadata: %w", err)
	}

	job := &domain.Job{
		ID:         uuid.NewString(),
		DocumentID: doc.ID,
		OwnerID:    ownerID,
		Status:     domain.JobPending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := s.Jobs.Create(ctx, job); err != nil {
		return "", fmt.Errorf("save job: %w", err)
	}

	if err := s.Queue.Publish(ctx, ports.QueueMessage{JobID: job.ID, Attempt: 1}); err != nil {
		return "", fmt.Errorf("publish job: %w", err)
	}

	return job.ID, nil
}
