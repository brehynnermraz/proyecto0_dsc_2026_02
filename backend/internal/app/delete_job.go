package app

import (
	"context"
	"fmt"

	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

// DeleteJobService borra un job junto con todo lo que le pertenece: el
// documento original y, si existe, el bundle generado — sus filas en la
// base de datos y sus objetos en el almacenamiento. La autorización (que el
// job pertenezca a quien pide borrarlo) es responsabilidad del handler
// HTTP, igual que en el resto de operaciones sobre jobs.
type DeleteJobService struct {
	Jobs      ports.JobRepository
	Documents ports.DocumentRepository
	Bundles   ports.BundleRepository
	Storage   ports.ObjectStore
}

func (s *DeleteJobService) Delete(ctx context.Context, job *domain.Job) error {
	if job.BundleID != nil {
		if bundle, err := s.Bundles.FindByID(ctx, *job.BundleID); err == nil {
			_ = s.Storage.DeleteBundle(ctx, bundle.StorageKey)
		}

		// Ver JobRepository.ClearBundle: la FK circular entre jobs y bundles
		// obliga a romper la referencia antes de poder borrar el bundle.
		if err := s.Jobs.ClearBundle(ctx, job.ID); err != nil {
			return fmt.Errorf("clear bundle ref: %w", err)
		}
		if err := s.Bundles.Delete(ctx, *job.BundleID); err != nil {
			return fmt.Errorf("delete bundle: %w", err)
		}
	}

	if err := s.Jobs.Delete(ctx, job.ID); err != nil {
		return fmt.Errorf("delete job: %w", err)
	}

	doc, err := s.Documents.FindByID(ctx, job.DocumentID)
	if err != nil {
		// El job ya se borró; que falte el documento (por ejemplo, un
		// borrado anterior a medias) no debe impedir considerar la
		// operación exitosa.
		return nil
	}
	_ = s.Storage.DeleteOriginal(ctx, doc.StorageKey)
	return s.Documents.Delete(ctx, doc.ID)
}
