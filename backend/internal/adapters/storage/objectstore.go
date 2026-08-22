// Package storage implementa el puerto ports.ObjectStore hablando por HTTP con
// el servicio object-storage (../object-storage), el mismo almacén que usa el
// worker. Antes esto era un cliente de MinIO; se cambió para que la API y el
// worker escriban y lean del MISMO almacén (si no, el worker no encontraría el
// original que la API cargó).
//
// Contrato del servicio (ver object-storage/INTEGRACION-WORKER.md):
//
//	PUT    /v1/objects/{key}      201  guarda el objeto
//	GET    /v1/objects/{key}      200  descarga el objeto
//	DELETE /v1/objects/{key}      204  borra el objeto
//	GET    /v1/objects?prefix={p} 200  {"objects":[{"key":...}, ...]}
//
// Todas exigen Authorization: Bearer <STORAGE_TOKEN>.
package storage

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"okfbundler/internal/ports"
)

type ObjectStore struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewObjectStore arma el cliente. baseURL es p. ej. http://localhost:9000 (o
// http://object-storage:9000 dentro de Compose); token es el STORAGE_TOKEN
// compartido con el servicio y con el worker.
func NewObjectStore(baseURL, token string) *ObjectStore {
	return &ObjectStore{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{},
	}
}

var _ ports.ObjectStore = (*ObjectStore)(nil)

// objectURL escapa cada segmento de la clave pero conserva las barras como
// separadores de ruta (igual que el HTTPStore del worker).
func (s *ObjectStore) objectURL(key string) string {
	var b strings.Builder
	b.WriteString(s.baseURL)
	b.WriteString("/v1/objects")
	for _, seg := range strings.Split(key, "/") {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(seg))
	}
	return b.String()
}

func (s *ObjectStore) PutOriginal(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key), r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if size >= 0 {
		req.ContentLength = size
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("object-storage PUT %s: estado %d", key, resp.StatusCode)
	}
	return nil
}

func (s *ObjectStore) DeleteOriginal(ctx context.Context, key string) error {
	return s.deleteKey(ctx, key)
}

// DeleteBundle borra todos los objetos bajo el prefijo (index.md, log.md, los
// conceptos...). Best-effort: no falla si el bundle ya no tiene objetos.
func (s *ObjectStore) DeleteBundle(ctx context.Context, prefix string) error {
	keys, err := s.list(ctx, prefix+"/")
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.deleteKey(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// GetBundleStream lista el prefijo del bundle y empaqueta en zip, sobre la
// marcha, todos sus objetos. El listado se hace antes de devolver el reader
// para poder reportar "no existe" como error real: si esperáramos a que fallara
// el primer Read, el handler ya habría comprometido un 200 con cuerpo vacío.
// El zip se arma en un goroutine escribiendo a un io.Pipe, sin materializar el
// paquete completo en memoria.
func (s *ObjectStore) GetBundleStream(ctx context.Context, prefix string) (io.ReadCloser, error) {
	keys, err := s.list(ctx, prefix+"/")
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("bundle no encontrado: %s", prefix)
	}

	pr, pw := io.Pipe()
	go func() {
		zw := zip.NewWriter(pw)
		var streamErr error
		for _, key := range keys {
			streamErr = s.copyIntoZip(ctx, zw, key, strings.TrimPrefix(key, prefix+"/"))
			if streamErr != nil {
				break
			}
		}
		if closeErr := zw.Close(); streamErr == nil {
			streamErr = closeErr
		}
		pw.CloseWithError(streamErr)
	}()

	return pr, nil
}

func (s *ObjectStore) copyIntoZip(ctx context.Context, zw *zip.Writer, key, nameInZip string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("object-storage GET %s: estado %d", key, resp.StatusCode)
	}

	w, err := zw.Create(nameInZip)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (s *ObjectStore) deleteKey(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp.Body)

	// 204 = borrado; 404 = ya no estaba, lo damos por bueno (idempotente).
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("object-storage DELETE %s: estado %d", key, resp.StatusCode)
	}
	return nil
}

// list devuelve las claves bajo un prefijo consultando GET /v1/objects?prefix=.
func (s *ObjectStore) list(ctx context.Context, prefix string) ([]string, error) {
	u := s.baseURL + "/v1/objects?prefix=" + url.QueryEscape(prefix)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer drain(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("object-storage LIST %s: estado %d", prefix, resp.StatusCode)
	}

	var body struct {
		Objects []struct {
			Key string `json:"key"`
		} `json:"objects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(body.Objects))
	for _, o := range body.Objects {
		keys = append(keys, o.Key)
	}
	return keys, nil
}

// drain agota y cierra el cuerpo para que el http.Client pueda reutilizar la
// conexión (keep-alive).
func drain(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
