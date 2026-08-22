// Package httpapi expone el almacén como una API HTTP: traduce peticiones a
// llamadas sobre store.Store y errores del almacén a códigos de estado.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniandes-isis4426/okf/objectstore/internal/config"
	"github.com/uniandes-isis4426/okf/objectstore/internal/store"
)

// Server agrupa las dependencias de los handlers. Se construye una vez y su método
// Handler devuelve el router ya cableado.
type Server struct {
	store    store.Store
	token    string
	maxBytes int64
	log      *slog.Logger
}

func New(st store.Store, cfg config.Config, log *slog.Logger) *Server {
	return &Server{
		store:    st,
		token:    cfg.Token,
		maxBytes: cfg.MaxObjectBytes,
		log:      log,
	}
}

// Handler construye el router. Desde Go 1.22 el ServeMux enruta por método y
// admite el comodín {key...}, que captura el resto de la ruta —incluidas las
// barras— como la clave del objeto.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// /healthz queda fuera de la autenticación: es la sonda de Compose.
	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.Handle("PUT /v1/objects/{key...}", s.authed(s.handlePut))
	mux.Handle("GET /v1/objects/{key...}", s.authed(s.handleGet))
	mux.Handle("HEAD /v1/objects/{key...}", s.authed(s.handleHead))
	mux.Handle("DELETE /v1/objects/{key...}", s.authed(s.handleDelete))
	mux.Handle("GET /v1/objects", s.authed(s.handleList))

	return mux
}

// authed envuelve un handler exigiendo el token Bearer antes de ejecutarlo.
func (s *Server) authed(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "token ausente o inválido")
			return
		}
		next(w, r)
	})
}

// authorized compara el token con comparación en tiempo constante: un compare byte
// a byte que corta al primer fallo filtraría, por el tiempo de respuesta, cuántos
// caracteres del token acertó un atacante.
func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimPrefix(h, prefix)
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	// El tope se aplica aquí, no en el store: el store escribe lo que le den, y es
	// la frontera HTTP la que corta la entrada no confiable. Al superarlo, Read
	// devuelve un *http.MaxBytesError.
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytes)

	info, err := s.store.Put(r.Context(), key, r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "el objeto excede el tamaño máximo")
			return
		}
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	rc, info, err := s.store.Get(r.Context(), r.PathValue("key"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	defer rc.Close()

	writeObjectHeaders(w, info)
	w.WriteHeader(http.StatusOK)
	// Un fallo aquí llega con la cabecera 200 ya enviada: no se puede cambiar el
	// estado, solo registrarlo.
	if _, err := io.Copy(w, rc); err != nil {
		s.log.Error("copiando el cuerpo del objeto", "key", info.Key, "err", err)
	}
}

func (s *Server) handleHead(w http.ResponseWriter, r *http.Request) {
	info, err := s.store.Head(r.Context(), r.PathValue("key"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeObjectHeaders(w, info)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.Context(), r.PathValue("key")); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	infos, err := s.store.List(r.Context(), r.URL.Query().Get("prefix"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	if infos == nil {
		infos = []store.ObjectInfo{}
	}
	writeJSON(w, http.StatusOK, listResponse{Objects: infos})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// storeError traduce los centinelas del store a códigos HTTP. El detalle de un 500
// se registra pero no se devuelve al cliente: podría revelar rutas internas.
func (s *Server) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "objeto no encontrado")
	case errors.Is(err, store.ErrInvalidKey):
		writeError(w, http.StatusBadRequest, "clave de objeto inválida")
	default:
		s.log.Error("error interno del almacén", "err", err)
		writeError(w, http.StatusInternalServerError, "error interno")
	}
}

type listResponse struct {
	Objects []store.ObjectInfo `json:"objects"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeObjectHeaders(w http.ResponseWriter, info store.ObjectInfo) {
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("Last-Modified", info.ModTime.UTC().Format(http.TimeFormat))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
