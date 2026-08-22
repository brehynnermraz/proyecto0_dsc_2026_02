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
	// El MIME lo trae la BD (lo puso la API a partir del "format" del usuario).
	// Si el usuario se equivocó (subir un .epub como HTML), ese MIME miente y el
	// original quedaría destrozado por el parser equivocado. Antes de elegir
	// parser, se olfatean los magic bytes: un EPUB se reconoce por su firma y se
	// procesa como EPUB pase lo que pase el MIME declarado.
	mime := detectMIME(raw, src.MIME)

	p, err := parser.For(mime)
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
		fmt.Sprintf("parseo del original (%s)", mime),
		fmt.Sprintf("segmentación en %d unidad(es)", len(units)),
		"render de conceptos e índice (Markdown)",
	}
	if mime != src.MIME {
		// Trazabilidad: el log deja constancia de que el MIME declarado no casaba
		// con el contenido y se corrigió.
		ops = append([]string{
			fmt.Sprintf("MIME corregido por contenido: %s → %s", src.MIME, mime),
		}, ops...)
	}

	v := Validate(units, files)
	files["log.md"] = RenderLog(src, title, units, v, ops)

	return Result{Files: files, Validation: v, Units: len(units)}, nil
}

// detectMIME corrige el MIME declarado cuando el contenido lo desmiente. Hoy
// solo detecta EPUB (el caso real que rompió: un .epub subido como text/html);
// para el resto confía en lo declarado.
func detectMIME(raw []byte, declared string) string {
	if isEPUB(raw) {
		return "application/epub+zip"
	}
	return declared
}

// isEPUB reconoce un EPUB por su firma canónica: un ZIP (magic "PK\x03\x04")
// cuyo PRIMER archivo es "mimetype" con contenido "application/epub+zip",
// almacenado sin comprimir en un offset fijo (así lo exige el OCF del EPUB).
// Es una comprobación barata que no descomprime nada.
func isEPUB(raw []byte) bool {
	const (
		mimeType = "application/epub+zip"
		nameOff  = 30 // tras la cabecera local del ZIP viene el nombre del archivo
		nameLen  = len("mimetype")
	)
	if len(raw) < nameOff+nameLen+len(mimeType) {
		return false
	}
	if raw[0] != 'P' || raw[1] != 'K' || raw[2] != 0x03 || raw[3] != 0x04 {
		return false
	}
	if string(raw[nameOff:nameOff+nameLen]) != "mimetype" {
		return false
	}
	return string(raw[nameOff+nameLen:nameOff+nameLen+len(mimeType)]) == mimeType
}
