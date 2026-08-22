// html.go — parser de text/html con golang.org/x/net/html: los h1/h2 marcan unidades.
// Se implementa en el PASO 3 (semana 2).

package parser

import (
    "bytes"
    "fmt"
    "strings"

    "golang.org/x/net/html"
    "golang.org/x/net/html/atom"

    "github.com/uniandes-isis4426/okf/worker/internal/domain"
)

type HTML struct{}

var _ Parser = HTML{}

func (HTML) Parse(raw []byte, filename string) (domain.Document, error){
	root, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return domain.Document{}, fmt.Errorf("%w: %v", ErrMalformedInput, err)
	}

	b := &htmlBuilder{}
	b.walk(root)
	b.flush() // flush last unit

	title := b.docTitle
	if title == "" && len(b.units) > 0 {
		title = b.units[0].Title
	}

	if title == "" {
		title = strings.TrimSuffix(filename, ".html")
	}
	return domain.Document{
		Title: title,
		Units: b.units,
	}, nil
}

type htmlBuilder struct {
	units []domain.Unit
	docTitle string
	cur *domain.Unit
	buf strings.Builder
}

func (b *htmlBuilder) walk(n *html.Node) {
	if n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.Title:
			b.docTitle = strings.TrimSpace(textOf(n))
			return
		case atom.H1, atom.H2:
			b.flush()
			level := 1
			if n.DataAtom == atom.H2 {
				level = 2
			}
			b.cur = &domain.Unit{
				Title: strings.TrimSpace(textOf(n)),
				Level: level,
			}
			return
		case atom.P:
			b.line(strings.TrimSpace(textOf(n)))
			return
		case atom.Li:
			b.line("- " + strings.TrimSpace(textOf(n)))
			return
		case atom.Pre, atom.Code:
			b.line("```\n" + strings.TrimSpace(textOf(n)) + "\n```")
			return
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.walk(c)
	}
}

// line añade una línea de Markdown a la unidad actual, creando una unidad
// inicial sin título si el contenido aparece antes del primer encabezado (§ regla 4 del segmentador).
func (b *htmlBuilder) line(s string) {
    if s == "" {
        return
    }
    if b.cur == nil {
        b.cur = &domain.Unit{Level: 1}
    }
    b.buf.WriteString(s)
    b.buf.WriteString("\n\n")
}

func (b *htmlBuilder) flush() {
	if b.cur == nil {
		return
	}
	b.cur.Content = strings.TrimSpace(b.buf.String())
	b.units = append(b.units, *b.cur)
	b.cur = nil
	b.buf.Reset()
}

func textOf(n *html.Node) string {
	var sb strings.Builder
	var rec func(*html.Node)
	rec = func(m *html.Node) {
		if m.Type == html.TextNode {
			sb.WriteString(m.Data)
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return sb.String()
}