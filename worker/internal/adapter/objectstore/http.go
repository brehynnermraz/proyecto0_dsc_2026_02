// http.go — backend de Store que habla con el servicio object-storage por HTTP, sin
// compartir disco. Es la implementación real que sustituye a fs/minio: el worker deja
// de tocar una carpeta y hace GET/PUT contra la API /v1/objects del servicio.
//
// El resto del worker no cambia: solo ve el puerto Store.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HTTPStore implementa Store hablando con el servicio object-storage por HTTP.
type HTTPStore struct {
	baseURL string // p. ej. http://object-storage:9000 (sin barra final)
	token   string // STORAGE_TOKEN compartido con el servicio y la API
	client  *http.Client
}

var _ Store = (*HTTPStore)(nil)

// NewHTTP arma el adaptador. No fija Timeout en el cliente a propósito: Get devuelve el
// cuerpo en streaming y un timeout global cortaría la lectura de un original grande. El
// plazo lo pone el contexto que pasa cada llamada (el ctx de la entrega / del seed).
func NewHTTP(baseURL, token string) *HTTPStore {
	return &HTTPStore{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{},
	}
}

// objectURL escapa cada segmento de la clave pero conserva las barras como separadores
// de ruta (originals/owner/doc → /v1/objects/originals/owner/doc).
func (s *HTTPStore) objectURL(key string) string {
	var b strings.Builder
	b.WriteString(s.baseURL)
	b.WriteString("/v1/objects")
	for _, seg := range strings.Split(key, "/") {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(seg))
	}
	return b.String()
}

func (s *HTTPStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Body, nil // el llamador cierra el cuerpo
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNotFound // centinela que el núcleo entiende sin saber de HTTP
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("object-storage GET %s: estado %d", key, resp.StatusCode)
	}
}

func (s *HTTPStore) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key), r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// Con size >= 0 se envía Content-Length; con -1 el cliente usa chunked.
	if size >= 0 {
		req.ContentLength = size
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drenar el cuerpo permite reutilizar la conexión keep-alive en la siguiente llamada.
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("object-storage PUT %s: estado %d", key, resp.StatusCode)
	}
	return nil
}
