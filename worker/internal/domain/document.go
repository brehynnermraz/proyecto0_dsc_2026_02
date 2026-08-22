// Package domain contiene los tipos del dominio del worker tal como los ve la lógica
// de negocio, independientes de cómo se guardan o transportan. No importa
// infraestructura (ni pgx, ni amqp, ni html): por eso el conversor se puede probar con
// `go test` en milisegundos.
package domain

// Document es el resultado de analizar el archivo original, ANTES de segmentar.
type Document struct {
	Title string
	Units []Unit // en el orden del documento de origen
}

// Unit es una unidad lógica del documento (un concepto del bundle).
type Unit struct {
	Level   int // 1 o 2
	Title   string
	Content string // markdown del cuerpo, sin el encabezado
}

// BundleFiles es el bundle en memoria: ruta relativa dentro del bundle → contenido.
// Al final del pipeline las claves esperadas son "index.md", "log.md" y un NN-slug.md
// por concepto.
type BundleFiles map[string][]byte

// ValidationResult es la salida del validador. Los errores impiden la publicación; las
// advertencias no. Status usa el enum ValidationStatus definido en job.go.
type ValidationResult struct {
	Status   ValidationStatus
	Warnings []string
	Errors   []string
}
