package converter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
)

// oversizedBytes marca un concepto desproporcionadamente grande (advertencia, no error).
const oversizedBytes = 100_000

// mdLink captura enlaces markdown [texto](destino).
var mdLink = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

// Validate clasifica el bundle en los tres niveles del §6. Recibe las unidades (para
// las advertencias de contenido) y los archivos ya renderizados EXCEPTO log.md (que se
// añade justo después): por eso no exige log.md, solo index.md + conceptos + enlaces.
//
// ERRORES ⇒ Invalid (no se publica ni se sube):
//   - falta index.md
//   - ningún documento de concepto
//   - un enlace relativo de index.md que no resuelve a un archivo del bundle
//
// ADVERTENCIAS ⇒ ValidWithWarnings (sí se publica):
//   - una unidad con cuerpo vacío
//   - títulos duplicados entre unidades
//   - un concepto desproporcionadamente grande
//
// Que un documento sin divisiones produzca UNA sola unidad NO es advertencia.
func Validate(units []domain.Unit, files domain.BundleFiles) domain.ValidationResult {
	var errs, warns []string

	// ---- Errores ----
	if _, ok := files["index.md"]; !ok {
		errs = append(errs, "falta index.md")
	}

	concepts := 0
	for name := range files {
		if name == "index.md" || name == "log.md" {
			continue
		}
		if strings.HasSuffix(name, ".md") {
			concepts++
		}
	}
	if concepts == 0 {
		errs = append(errs, "no hay ningún documento de concepto")
	}

	if idx, ok := files["index.md"]; ok {
		for _, m := range mdLink.FindAllSubmatch(idx, -1) {
			dest := string(m[1])
			if !isRelativeMarkdown(dest) {
				continue // enlaces externos o anclas no se comprueban
			}
			if _, ok := files[dest]; !ok {
				errs = append(errs, "enlace roto en index.md: "+dest)
			}
		}
	}

	// ---- Advertencias ----
	seen := map[string]bool{}
	for i, u := range units {
		if strings.TrimSpace(u.Content) == "" {
			warns = append(warns, fmt.Sprintf("unidad %d sin cuerpo: %q", i+1, u.Title))
		}
		key := strings.ToLower(strings.TrimSpace(u.Title))
		if key != "" && seen[key] {
			warns = append(warns, "título duplicado: "+u.Title)
		}
		seen[key] = true
	}
	for name, body := range files {
		if name == "index.md" || name == "log.md" {
			continue
		}
		if len(body) > oversizedBytes {
			warns = append(warns, fmt.Sprintf("concepto muy grande (%d bytes): %s", len(body), name))
		}
	}

	// ---- Veredicto ----
	status := domain.ValidationValid
	switch {
	case len(errs) > 0:
		status = domain.ValidationInvalid
	case len(warns) > 0:
		status = domain.ValidationValidWithWarnings
	}
	return domain.ValidationResult{Status: status, Warnings: warns, Errors: errs}
}

// isRelativeMarkdown indica si un destino de enlace apunta a un archivo .md del propio
// bundle (no http(s), no ancla, no ruta absoluta).
func isRelativeMarkdown(dest string) bool {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "/") {
		return false
	}
	if strings.Contains(dest, "://") {
		return false
	}
	return strings.HasSuffix(dest, ".md")
}
