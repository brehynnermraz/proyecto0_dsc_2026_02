// epub.go — parser de EPUB (application/epub+zip). Un EPUB es un ZIP con capítulos
// XHTML y un manifiesto .opf que declara el orden de lectura (spine). Este parser:
//  1. localiza el .opf vía META-INF/container.xml,
//  2. lee del .opf el título, el manifest (id → href) y el spine (orden),
//  3. recorre los capítulos EN ORDEN y reutiliza el walker HTML para extraer unidades.
package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"

	"golang.org/x/net/html"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
)

type EPUB struct{}

var _ Parser = EPUB{}

// container refleja META-INF/container.xml (solo el full-path del primer rootfile).
type container struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

// opfPackage refleja lo que necesitamos del .opf. Las etiquetas xml casan por nombre
// local, así que `title` casa con <dc:title> sin declarar el namespace.
type opfPackage struct {
	Title string `xml:"metadata>title"`
	Items []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
	} `xml:"manifest>item"`
	Itemrefs []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

func (EPUB) Parse(raw []byte, filename string) (domain.Document, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return domain.Document{}, fmt.Errorf("%w: epub no es un zip válido: %v", ErrMalformedInput, err)
	}
	byName := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		byName[f.Name] = f
	}

	// 1. Localizar el .opf.
	var cont container
	if err := readXML(byName["META-INF/container.xml"], &cont); err != nil {
		return domain.Document{}, fmt.Errorf("%w: container.xml: %v", ErrMalformedInput, err)
	}
	if len(cont.Rootfiles) == 0 || cont.Rootfiles[0].FullPath == "" {
		return domain.Document{}, fmt.Errorf("%w: epub sin rootfile en container.xml", ErrMalformedInput)
	}
	opfPath := cont.Rootfiles[0].FullPath

	// 2. Leer el .opf: título, manifest y spine.
	var pkg opfPackage
	if err := readXML(byName[opfPath], &pkg); err != nil {
		return domain.Document{}, fmt.Errorf("%w: opf %s: %v", ErrMalformedInput, opfPath, err)
	}
	href := make(map[string]string, len(pkg.Items))
	for _, it := range pkg.Items {
		href[it.ID] = it.Href
	}
	opfDir := path.Dir(opfPath) // las hrefs son relativas a la carpeta del .opf

	// 3. Recorrer el spine en orden, acumulando unidades entre capítulos.
	b := &htmlBuilder{}
	for _, ir := range pkg.Itemrefs {
		h := href[ir.IDRef]
		if h == "" {
			continue
		}
		name := h
		if opfDir != "." && opfDir != "" {
			name = path.Join(opfDir, h)
		}
		f := byName[name]
		if f == nil {
			continue
		}
		data, err := readFile(f)
		if err != nil {
			return domain.Document{}, fmt.Errorf("%w: leyendo %s: %v", ErrMalformedInput, name, err)
		}
		root, err := html.Parse(bytes.NewReader(data))
		if err != nil {
			continue // un capítulo ilegible no tumba el libro entero
		}
		b.walk(root)
		b.flush() // límite de capítulo: no mezclar el final de uno con el inicio del siguiente
	}
	b.flush()

	if len(b.units) == 0 {
		return domain.Document{}, fmt.Errorf("%w: epub sin contenido legible", ErrMalformedInput)
	}

	title := strings.TrimSpace(pkg.Title)
	if title == "" {
		title = strings.TrimSuffix(filename, path.Ext(filename))
	}
	return domain.Document{Title: title, Units: b.units}, nil
}

// readXML abre una entrada del zip y la decodifica como XML.
func readXML(f *zip.File, v any) error {
	if f == nil {
		return fmt.Errorf("entrada ausente")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(rc).Decode(v)
}

// readFile abre una entrada del zip y lee todo su contenido.
func readFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
