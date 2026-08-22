// Package store define el puerto de persistencia del servicio y su implementación
// en sistema de archivos.
//
// La capa HTTP habla contra la interfaz Store y nunca contra el backend concreto:
// cambiar el sistema de archivos por otro almacén no obliga a tocar los handlers.
package store

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	// ErrNotFound: el objeto no existe. La capa HTTP lo traduce a 404.
	ErrNotFound = errors.New("objeto no encontrado")

	// ErrInvalidKey: la clave (o el prefijo) no es utilizable. La capa HTTP lo
	// traduce a 400. Es la defensa contra el path traversal: la clave llega de
	// datos que originó un usuario y no puede escapar de la raíz.
	ErrInvalidKey = errors.New("clave de objeto inválida")
)

// ObjectInfo son los metadatos que el servicio expone en HEAD, GET y List. Las
// etiquetas json fijan el contrato de la API en minúsculas.
type ObjectInfo struct {
	Key         string    `json:"key"`
	Size        int64     `json:"size"`
	ContentType string    `json:"contentType"`
	ModTime     time.Time `json:"modTime"`
}

// Store es el puerto de almacenamiento. Cinco operaciones cubren todo el flujo:
// escribir el original y cada archivo del bundle (Put), leerlos (Get), consultar
// su existencia (Head), enumerar un bundle para empaquetarlo (List) y limpiar tras
// un fallo (Delete).
type Store interface {
	// Put escribe el objeto de forma atómica y devuelve sus metadatos.
	Put(ctx context.Context, key string, r io.Reader) (ObjectInfo, error)

	// Get abre el objeto para lectura. El llamador debe cerrar el ReadCloser.
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)

	// Head devuelve los metadatos sin abrir el contenido.
	Head(ctx context.Context, key string) (ObjectInfo, error)

	// Delete borra el objeto. Devuelve ErrNotFound si no existía.
	Delete(ctx context.Context, key string) error

	// List enumera los objetos cuya clave empieza por prefix. Un prefix vacío
	// enumera todo el almacén.
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
}
