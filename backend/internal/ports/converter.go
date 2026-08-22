package ports

import (
	"context"
	"io"

	"okfbundler/internal/domain"
)

// DocumentConverter segmenta un documento de un formato concreto en unidades
// lógicas. ProcessJobService elige el converter registrado que declare
// soporte para el formato del documento.
type DocumentConverter interface {
	Supports(format string) bool
	Convert(ctx context.Context, r io.Reader) ([]domain.Unit, error)
}
