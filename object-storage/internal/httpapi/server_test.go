package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uniandes-isis4426/okf/objectstore/internal/config"
	"github.com/uniandes-isis4426/okf/objectstore/internal/store"
)

const testToken = "tok-de-prueba"

// newServer levanta el servicio real (con FSStore sobre carpeta temporal) tras un
// httptest.Server, que escucha en un puerto libre solo durante el test.
func newServer(t *testing.T, maxBytes int64) *httptest.Server {
	t.Helper()
	st, err := store.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	cfg := config.Config{Token: testToken, MaxObjectBytes: maxBytes}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // silencia el log en tests
	srv := httptest.NewServer(New(st, cfg, logger).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// do arma una petición con (o sin) token y devuelve la respuesta.
func do(t *testing.T, method, url, token, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

func TestRoundtrip(t *testing.T) {
	srv := newServer(t, 1<<20)
	url := srv.URL + "/v1/objects/bundles/u1/b1/index.md"

	resp := do(t, http.MethodPut, url, testToken, "hola")
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT = %d, quiero 201", resp.StatusCode)
	}

	resp = do(t, http.MethodGet, url, testToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET = %d, quiero 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hola" {
		t.Errorf("cuerpo = %q, quiero \"hola\"", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("Content-Type = %q, quiero text/markdown", ct)
	}
}

func TestAuth(t *testing.T) {
	srv := newServer(t, 1<<20)
	url := srv.URL + "/v1/objects/bundles/u1/b1/index.md"

	casos := []struct {
		nombre string
		token  string
	}{
		{"sin token", ""},
		{"token incorrecto", "otro"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			resp := do(t, http.MethodGet, url, c.token, "")
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s = %d, quiero 401", c.nombre, resp.StatusCode)
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	srv := newServer(t, 1<<20)
	resp := do(t, http.MethodGet, srv.URL+"/v1/objects/no/existe.md", testToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET inexistente = %d, quiero 404", resp.StatusCode)
	}
}

func TestDemasiadoGrande(t *testing.T) {
	srv := newServer(t, 4) // tope de 4 bytes
	url := srv.URL + "/v1/objects/bundles/u1/b1/grande.md"
	resp := do(t, http.MethodPut, url, testToken, "mucho mas de cuatro bytes")
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("PUT gigante = %d, quiero 413", resp.StatusCode)
	}
}

func TestHealthSinToken(t *testing.T) {
	srv := newServer(t, 1<<20)
	resp := do(t, http.MethodGet, srv.URL+"/healthz", "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz = %d, quiero 200", resp.StatusCode)
	}
}
