// Package parser convierte los bytes del documento original en un *converter.Document
// con sus unidades en orden. Un parser por MIME; núcleo puro, sin E/S.
//
//	registry.go  — For(mime) devuelve el parser adecuado.
//	               Un MIME no soportado -> job.ErrPermanent (no tiene sentido reintentar).
//	html.go      — text/html         (golang.org/x/net/html)                 
//	epub.go      — application/epub+zip (archive/zip + xml + el walker HTML)  
//
// Los demás formatos quedan pendientes (ver PENDIENTES.md en la raíz):
//	markdown.go  — text/markdown, text.go — text/plain, docx.go — OOXML, pdf.go — application/pdf.
package parser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
)

type Parser interface {
    Parse(raw []byte, filename string) (domain.Document, error)
}

var registry = map[string]Parser{
	"text/html":            HTML{},
	"application/epub+zip": EPUB{},
}

var ErrUnsupportedMIME = errors.New("formato no soportado") // → job.ErrPermanent
var ErrMalformedInput  = errors.New("archivo ilegible o corrupto")

func For(mime string) (Parser, error) {
    key := strings.ToLower(strings.TrimSpace(mime))
    if i := strings.IndexByte(key, ';'); i >= 0 {
        key = strings.TrimSpace(key[:i])
    }
    p, ok := registry[key]
    if !ok {
        return nil, fmt.Errorf("%w: %s", ErrUnsupportedMIME, mime)
    }
    return p, nil
}