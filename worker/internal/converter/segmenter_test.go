package converter

import (
	"os"
	"strings"
	"testing"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
)

// h construye una unidad "encabezado" de un nivel dado (como las que produce el
// parser antes de segmentar).
func h(level int, title string) domain.Unit {
	return domain.Unit{Level: level, Title: title, Content: "cuerpo de " + title}
}

func titles(units []domain.Unit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.Title
	}
	return out
}

func TestSegmentCortaEnNivelQueDivide(t *testing.T) {
	cases := []struct {
		name  string
		units []domain.Unit
		want  []string // títulos de los conceptos resultantes, en orden
	}{
		{
			// Libro: h1 título (1 vez), h2 partes (repite), h3 capítulos (repite y
			// más profundo) -> corta por capítulo.
			name: "libro h1/h2/h3 corta en h3",
			units: []domain.Unit{
				h(1, "The Idiot"),
				h(2, "PART I"), h(3, "I"), h(3, "II"),
				h(2, "PART II"), h(3, "I"), h(3, "II"),
			},
			want: []string{"The Idiot", "I", "II", "I", "II"},
		},
		{
			// Guía: h1 repite (2 capítulos), h2 aparece una vez -> corta en h1, el
			// h2 se pliega. Debe seguir dando 2 conceptos (no regresión del mock).
			name: "guia h1 repetido corta en h1",
			units: []domain.Unit{
				h(1, "Introducción a la nube"),
				h(2, "Modelos de servicio"),
				h(1, "Orquestación de contenedores"),
			},
			want: []string{"Introducción a la nube", "Orquestación de contenedores"},
		},
		{
			// Un solo h2, sin repetición -> cae al nivel con título más profundo (2).
			name:  "un unico encabezado",
			units: []domain.Unit{h(2, "Mensajería asíncrona")},
			want:  []string{"Mensajería asíncrona"},
		},
		{
			// Sin títulos -> una sola unidad (§6 regla 4). El contenido sin
			// encabezado propio toma el título de la unidad inicial.
			name:  "sin encabezados con titulo",
			units: []domain.Unit{{Level: 1, Content: "solo cuerpo"}},
			want:  []string{"Introducción"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := titles(Segment(domain.Document{Title: "", Units: tc.units}))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("Segment() = %v, quiero %v", got, tc.want)
			}
		})
	}
}

// TestConvertEpubMockPorCapitulo verifica el pipeline completo sobre el EPUB
// commiteado y, de paso, que el olfateo de MIME reprocesa un .epub aunque venga
// etiquetado como text/html.
func TestConvertEpubMockPorCapitulo(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/mocks/guia-nube.epub")
	if err != nil {
		t.Fatalf("leyendo el mock: %v", err)
	}

	for _, mime := range []string{"application/epub+zip", "text/html"} {
		res, err := Convert(raw, Source{Filename: "guia-nube.epub", MIME: mime, SizeBytes: int64(len(raw))})
		if err != nil {
			t.Fatalf("Convert(mime=%s): %v", mime, err)
		}
		if res.Units != 2 {
			t.Errorf("mime=%s: unidades=%d, quiero 2", mime, res.Units)
		}
		if _, ok := res.Files["index.md"]; !ok {
			t.Errorf("mime=%s: falta index.md", mime)
		}
		if _, ok := res.Files["log.md"]; !ok {
			t.Errorf("mime=%s: falta log.md", mime)
		}
	}
}

func TestIsEPUB(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/mocks/guia-nube.epub")
	if err != nil {
		t.Fatalf("leyendo el mock: %v", err)
	}
	if !isEPUB(raw) {
		t.Error("isEPUB(guia-nube.epub) = false, quiero true")
	}
	if isEPUB([]byte("<html><body>no soy un epub</body></html>")) {
		t.Error("isEPUB(html) = true, quiero false")
	}
	if detectMIME(raw, "text/html") != "application/epub+zip" {
		t.Error("detectMIME no corrigió un epub etiquetado como text/html")
	}
}
