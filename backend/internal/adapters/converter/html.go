package converter

import (
	"context"
	"errors"
	"io"

	"okfbundler/internal/domain"
)

// HTMLConverter queda como punto de extensión (alcance opcional, sección
// 5.2: soporte de varios formatos de entrada). Registrarlo en cmd/worker
// una vez implementado.
type HTMLConverter struct{}

func (HTMLConverter) Supports(format string) bool { return format == "html" }

func (HTMLConverter) Convert(ctx context.Context, r io.Reader) ([]domain.Unit, error) {
	return nil, errors.New("conversor HTML aún no implementado")
}
