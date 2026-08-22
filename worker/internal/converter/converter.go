// Package converter es el NÚCLEO PURO del worker: parser → segmentador → renderer →
// validador. No importa infraestructura (ni pgx, ni objectstore, ni amqp), solo
// internal/domain y su subpaquete parser. Esa pureza es lo que permite probar aquí
// todo el §6 con `go test` sin Docker.
package converter

import (
	"fmt"
	"time"

	"github.com/uniandes-isis4426/okf/worker/internal/converter/parser"
	"github.com/uniandes-isis4426/okf/worker/internal/domain"
)

// Source son los metadatos del original que el renderer necesita para index.md y
// log.md. Llegan desde internal/job; el conversor no los busca.
type Source struct {
	JobID     string
	Filename  string
	MIME      string
	SizeBytes int64
	Attempt   int32
	StartedAt time.Time
}

// Result es todo lo que produce el conversor: los archivos del bundle (en memoria) y
// el veredicto de la validación.
type Result struct {
	Files      domain.BundleFiles
	Validation domain.ValidationResult
	Units      int
}

// Convert ejecuta el pipeline completo sobre los bytes del original.
//
// ORDEN DE GENERACIÓN — la trampa del pipeline. log.md contiene el veredicto de la
// validación, pero también forma parte del bundle. La secuencia correcta es:
//
//  1. Parse    → domain.Document (elige parser por MIME)
//  2. Segment  → unidades (reglas del §6.3)
//  3. Render   → index.md + NN-slug.md  (todavía SIN log.md)
//  4. Validate → veredicto
//  5. RenderLog→ log.md con ese veredicto
//
// Validar antes del paso 5 no puede exigir log.md (aún no existe): por eso el validador
// comprueba index.md + conceptos + enlaces, y log.md se añade justo después, siempre.
//
// El error que devuelve (MIME no soportado, archivo corrupto) es SIEMPRE permanente:
// parse/segment/render son deterministas, reintentar daría el mismo resultado.
func Convert(raw []byte, src Source) (Result, error) {
	p, err := parser.For(src.MIME)
	if err != nil {
		return Result{}, err // parser.ErrUnsupportedMIME
	}
	doc, err := p.Parse(raw, src.Filename)
	if err != nil {
		return Result{}, err // parser.ErrMalformedInput
	}

	units := Segment(doc)

	title := doc.Title
	if title == "" {
		title = fallbackTitle(src.Filename)
	}

	files := domain.BundleFiles{}
	for name, body := range RenderConcepts(units) {
		files[name] = body
	}
	files["index.md"] = RenderIndex(title, units, src)

	ops := []string{
		fmt.Sprintf("parseo del original (%s)", src.MIME),
		fmt.Sprintf("segmentación en %d unidad(es)", len(units)),
		"render de conceptos e índice (Markdown)",
	}

	v := Validate(units, files)
	files["log.md"] = RenderLog(src, title, units, v, ops)

	return Result{Files: files, Validation: v, Units: len(units)}, nil
}
