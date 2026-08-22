package converter

import (
	"fmt"
	"strings"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
)

// RenderConcepts escribe un archivo Markdown por unidad, con el nombre NN-slug.md y un
// frontmatter YAML (title, order). Los nombres son estables: mismo documento → mismos
// nombres, en el mismo orden.
func RenderConcepts(units []domain.Unit) domain.BundleFiles {
	files := make(domain.BundleFiles, len(units))
	for i, u := range units {
		name := FileName(i+1, u.Title)
		var b strings.Builder
		b.WriteString("---\n")
		fmt.Fprintf(&b, "title: %q\n", u.Title)
		fmt.Fprintf(&b, "order: %d\n", i+1)
		b.WriteString("---\n\n")
		fmt.Fprintf(&b, "# %s\n", u.Title)
		if body := strings.TrimSpace(u.Content); body != "" {
			b.WriteString("\n")
			b.WriteString(body)
			b.WriteString("\n")
		}
		files[name] = []byte(b.String())
	}
	return files
}

// RenderIndex escribe index.md: título, procedencia y la navegación a los conceptos EN
// EL ORDEN ORIGINAL. Los enlaces deben resolver a archivos que existan en el bundle
// (el validador lo comprueba; un enlace roto vuelve el bundle inválido).
func RenderIndex(title string, units []domain.Unit, src Source) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "Bundle generado a partir de `%s` (%d concepto(s)).\n\n", src.Filename, len(units))
	b.WriteString("## Conceptos\n\n")
	for i, u := range units {
		fmt.Fprintf(&b, "%d. [%s](%s)\n", i+1, u.Title, FileName(i+1, u.Title))
	}
	return []byte(b.String())
}

// RenderLog escribe log.md, la trazabilidad del §6: operaciones efectuadas, unidades
// detectadas, transformaciones aplicadas y el resultado de la validación. Se genera
// DESPUÉS de validar, porque incluye el veredicto.
func RenderLog(src Source, title string, units []domain.Unit, v domain.ValidationResult, ops []string) []byte {
	var b strings.Builder
	b.WriteString("# Registro de conversión\n\n")
	fmt.Fprintf(&b, "Documento: **%s**  \n", title)
	fmt.Fprintf(&b, "Origen: `%s` (%s, %d bytes)  \n", src.Filename, src.MIME, src.SizeBytes)
	fmt.Fprintf(&b, "Trabajo: `%s` · intento %d\n\n", src.JobID, src.Attempt)

	b.WriteString("## Operaciones\n\n")
	for _, op := range ops {
		fmt.Fprintf(&b, "- %s\n", op)
	}

	fmt.Fprintf(&b, "\n## Unidades detectadas (%d)\n\n", len(units))
	for i, u := range units {
		fmt.Fprintf(&b, "%d. %s (nivel %d)\n", i+1, u.Title, u.Level)
	}

	b.WriteString("\n## Validación\n\n")
	fmt.Fprintf(&b, "- Estado: **%s**\n", v.Status)
	b.WriteString("- Advertencias: ")
	b.WriteString(bulletsOrNone(v.Warnings, "ninguna"))
	b.WriteString("- Errores: ")
	b.WriteString(bulletsOrNone(v.Errors, "ninguno"))
	return []byte(b.String())
}

// bulletsOrNone imprime la palabra "vacío" dada o una lista con sangría.
func bulletsOrNone(items []string, none string) string {
	if len(items) == 0 {
		return none + "\n"
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, it := range items {
		fmt.Fprintf(&b, "  - %s\n", it)
	}
	return b.String()
}
