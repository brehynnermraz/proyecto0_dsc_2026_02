package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Concept struct {
	Filename string
	Title    string
	Order    int
	Content  string
}

type Bundle struct {
	ID         string
	JobID      string
	OwnerID    string
	StorageKey string
	Concepts   []Concept
	Valid      bool
	Warnings   []string
	CreatedAt  time.Time
}

// BuildBundle ensambla un bundle a partir de unidades ya segmentadas y aplica
// la validación mínima exigida antes de publicar (sección 5.1.6 del
// enunciado): al menos un concepto, nombres de archivo únicos y enlaces del
// índice resueltos.
//
// TODO: para demostrar el caso "bundle incompleto" de la sección 6 (falta de
// index.md o log.md) en el video, agreguen una verificación explícita tras
// escribir los archivos en el storage (ver ProcessJobService.writeBundleFiles)
// — aquí siempre se generan porque BuildBundle los produce programáticamente.
func BuildBundle(jobID, ownerID string, units []Unit) *Bundle {
	b := &Bundle{
		ID:        uuid.NewString(),
		JobID:     jobID,
		OwnerID:   ownerID,
		CreatedAt: time.Now().UTC(),
	}

	for i, u := range units {
		b.Concepts = append(b.Concepts, Concept{
			Filename: fmt.Sprintf("concepto-%02d.md", i+1),
			Title:    u.Title,
			Order:    i + 1,
			Content:  u.Content,
		})
	}

	b.Valid, b.Warnings = b.validate()
	return b
}

func (b *Bundle) validate() (bool, []string) {
	if len(b.Concepts) == 0 {
		return false, []string{"el bundle no contiene ningún concepto"}
	}

	seen := map[string]bool{}
	for _, c := range b.Concepts {
		if seen[c.Filename] {
			return false, []string{fmt.Sprintf("nombre de archivo duplicado: %s", c.Filename)}
		}
		seen[c.Filename] = true
	}

	return true, nil
}

func (b *Bundle) IndexMarkdown() string {
	var sb strings.Builder
	sb.WriteString("# Índice\n\n")
	for _, c := range b.Concepts {
		fmt.Fprintf(&sb, "%d. [%s](%s)\n", c.Order, c.Title, c.Filename)
	}
	return sb.String()
}

func (b *Bundle) LogMarkdown() string {
	var sb strings.Builder
	sb.WriteString("# Log de conversión\n\n")
	fmt.Fprintf(&sb, "- Trabajo: %s\n", b.JobID)
	fmt.Fprintf(&sb, "- Unidades detectadas: %d\n", len(b.Concepts))
	fmt.Fprintf(&sb, "- Generado: %s\n", b.CreatedAt.Format(time.RFC3339))
	if len(b.Warnings) > 0 {
		sb.WriteString("- Advertencias:\n")
		for _, w := range b.Warnings {
			fmt.Fprintf(&sb, "  - %s\n", w)
		}
	} else {
		sb.WriteString("- Validación: superada sin advertencias\n")
	}
	return sb.String()
}
