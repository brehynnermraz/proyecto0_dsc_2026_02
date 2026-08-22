package converter

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"okfbundler/internal/domain"
)

// MarkdownConverter segmenta por encabezados de primer nivel ("# "). Es el
// conversor recomendado por el enunciado (sección 11) para tener el flujo
// asíncrono de punta a punta funcionando desde la primera semana.
type MarkdownConverter struct{}

func (MarkdownConverter) Supports(format string) bool { return format == "markdown" }

// Convert nunca falla ni advierte por tener una sola unidad (sección 6,
// "documento breve"): un documento sin encabezados se vuelve un único
// concepto.
func (MarkdownConverter) Convert(ctx context.Context, r io.Reader) ([]domain.Unit, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var units []domain.Unit
	var title string
	var body strings.Builder

	flush := func() {
		if title == "" && body.Len() == 0 {
			return
		}
		if title == "" {
			title = fmt.Sprintf("Concepto %d", len(units)+1)
		}
		units = append(units, domain.Unit{Title: title, Content: strings.TrimSpace(body.String())})
		title = ""
		body.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "# ") {
			flush()
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()

	if len(units) == 0 {
		return nil, fmt.Errorf("el documento está vacío")
	}
	return units, nil
}
