package converter

import (
	"bytes"
	"context"
	"io"

	"okfbundler/internal/domain"
)

// PlainTextConverter es el conversor trivial: copia el texto completo a un
// único concepto, sin intentar detectar estructura.
type PlainTextConverter struct{}

func (PlainTextConverter) Supports(format string) bool { return format == "text" }

func (PlainTextConverter) Convert(ctx context.Context, r io.Reader) ([]domain.Unit, error) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r); err != nil {
		return nil, err
	}
	return []domain.Unit{{Title: "Documento", Content: buf.String()}}, nil
}
