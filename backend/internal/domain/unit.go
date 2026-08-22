package domain

// Unit es una unidad lógica ya segmentada por un DocumentConverter, antes de
// convertirse en un Concept con nombre de archivo dentro del bundle.
type Unit struct {
	Title   string
	Content string
}
