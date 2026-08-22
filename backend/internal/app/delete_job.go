package app

import (
	"context"
	"fmt"

	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

// DeleteJobService borra un job junto con todo lo que le pertenece: el
// documento original y, si existe, el bundle generado — sus filas y sus objetos
// en el object store. La autorización (que el job sea de quien lo borra) es
// del handler HTTP.
//
// En el esquema del worker no hay FK circular jobs<->bundles: es
// jobs.document_id -> documents y bundles.job_id -> jobs, ambas ON DELETE
// CASCADE. Por eso basta borrar el documento y la base arrastra el job y el
// bundle; aquí lo único que hay que hacer a mano es limpiar los objetos del
// store ANTES, porque el object store no participa del CASCADE.
type DeleteJobService struct {
	Jobs      ports.JobRepository
	Documents ports.DocumentRepository
	Bundles   ports.BundleRepository
	Storage   ports.ObjectStore
}

func (s *DeleteJobService) Delete(ctx context.Context, job *domain.Job) error {
	// 1. Objetos del bundle (si el worker ya publicó uno).
	if bundle, err := s.Bundles.FindByJob(ctx, job.ID); err == nil {
		_ = s.Storage.DeleteBundle(ctx, bundle.StoragePrefix)
	}

	// 2. Objeto original.
	if doc, err := s.Documents.FindByID(ctx, job.DocumentID); err == nil {
		_ = s.Storage.DeleteOriginal(ctx, doc.StorageKey)
	}

	// 3. Filas: borrar el documento arrastra job y bundle por CASCADE.
	if err := s.Documents.Delete(ctx, job.DocumentID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return nil
}
