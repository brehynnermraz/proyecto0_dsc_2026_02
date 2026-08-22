package converter

import (
	"fmt"
	"strings"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
)

// Segment parte el documento en unidades lógicas (§6.3), en este orden:
//
//  1. Cortar por encabezados de nivel 1. Si no hay ninguno (con título), cortar por
//     nivel 2.
//  2. Los encabezados de nivel inferior al de corte se pliegan como subsección (##)
//     dentro de la unidad en curso.
//  3. El contenido anterior al primer encabezado de corte forma una unidad inicial.
//  4. Si no hay encabezados de ningún nivel, se produce UNA SOLA unidad. Esto NO emite
//     advertencia: es una condición explícita del §6 y el error más fácil de cometer.
//  5. Se preserva el orden del documento de origen.
func Segment(d domain.Document) []domain.Unit {
	// Nivel de corte: 1 si hay algún encabezado de nivel 1 con título; si no, 2.
	cut := 2
	for _, u := range d.Units {
		if u.Level == 1 && strings.TrimSpace(u.Title) != "" {
			cut = 1
			break
		}
	}

	var out []domain.Unit
	for _, u := range d.Units {
		title := strings.TrimSpace(u.Title)
		body := strings.TrimSpace(u.Content)

		if title != "" && u.Level == cut {
			// Frontera: abre una unidad nueva.
			out = append(out, domain.Unit{Level: cut, Title: title, Content: body})
			continue
		}

		if len(out) == 0 {
			// Contenido anterior al primer encabezado de corte → unidad inicial.
			out = append(out, domain.Unit{
				Level:   1,
				Title:   initialTitle(title, d.Title),
				Content: body,
			})
			continue
		}

		// Encabezado más profundo que el de corte → subsección de la unidad en curso.
		last := &out[len(out)-1]
		var b strings.Builder
		b.WriteString(last.Content)
		if title != "" {
			b.WriteString("\n\n## ")
			b.WriteString(title)
		}
		if body != "" {
			b.WriteString("\n\n")
			b.WriteString(body)
		}
		last.Content = strings.TrimSpace(b.String())
	}

	if len(out) == 0 {
		out = append(out, domain.Unit{Level: 1, Title: fallbackTitle(d.Title), Content: ""})
	}
	return out
}

// initialTitle elige el título de la unidad inicial (contenido sin encabezado propio).
func initialTitle(own, docTitle string) string {
	if own != "" {
		return own
	}
	if strings.TrimSpace(docTitle) != "" {
		return strings.TrimSpace(docTitle)
	}
	return "Introducción"
}

// fallbackTitle da un título cuando no hay ninguno. Recibe el título del documento o,
// en Convert, el nombre de archivo.
func fallbackTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Documento"
	}
	// Quitar una extensión típica si viene un filename.
	for _, ext := range []string{".html", ".xhtml", ".epub", ".md", ".txt"} {
		s = strings.TrimSuffix(s, ext)
	}
	return s
}

// FileName construye el nombre de archivo de una unidad: NN-slug.md, con NN empezando
// en 01. El prefijo numérico garantiza el orden en el listado y evita colisiones cuando
// dos títulos producen el mismo slug.
func FileName(index int, title string) string {
	return fmt.Sprintf("%02d-%s.md", index, Slug(title))
}

// acentos mapea los diacríticos más comunes (español y algún vecino) a ASCII. El
// placeholder anterior los descartaba ("orquestación" → "orquestacin"); esto produce
// "orquestacion".
var acentos = map[rune]rune{
	'á': 'a', 'à': 'a', 'ä': 'a', 'â': 'a', 'ã': 'a',
	'é': 'e', 'è': 'e', 'ë': 'e', 'ê': 'e',
	'í': 'i', 'ì': 'i', 'ï': 'i', 'î': 'i',
	'ó': 'o', 'ò': 'o', 'ö': 'o', 'ô': 'o', 'õ': 'o',
	'ú': 'u', 'ù': 'u', 'ü': 'u', 'û': 'u',
	'ñ': 'n', 'ç': 'c',
}

// Slug normaliza un título para nombre de archivo: minúsculas, sin acentos, espacios y
// separadores por guiones, sin caracteres que compliquen una ruta. Guiones colapsados y
// recortados en los extremos.
func Slug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if folded, ok := acentos[r]; ok {
			r = folded
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '/':
			b.WriteByte('-')
		}
	}
	// Colapsar guiones repetidos y recortar.
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if out == "" {
		return "unidad"
	}
	return out
}
