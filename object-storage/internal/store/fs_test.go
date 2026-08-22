package store

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// mustFS crea un FSStore sobre una carpeta temporal que el runner de tests borra
// al terminar (t.TempDir). Así cada test corre aislado y sin dejar basura.
func mustFS(t *testing.T) *FSStore {
	t.Helper()
	s, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	return s
}

func TestPutThenGet(t *testing.T) {
	s := mustFS(t)
	ctx := context.Background()
	const key = "bundles/u1/b1/index.md"

	info, err := s.Put(ctx, key, strings.NewReader("hola"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if info.Size != 4 {
		t.Errorf("Size = %d, quiero 4", info.Size)
	}
	if !strings.HasPrefix(info.ContentType, "text/markdown") {
		t.Errorf("ContentType = %q, quiero text/markdown", info.ContentType)
	}

	rc, got, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != "hola" {
		t.Errorf("cuerpo = %q, quiero \"hola\"", body)
	}
	if got.Size != 4 {
		t.Errorf("Get Size = %d, quiero 4", got.Size)
	}
}

func TestPutSobrescribe(t *testing.T) {
	s := mustFS(t)
	ctx := context.Background()
	const key = "originals/u1/doc"

	if _, err := s.Put(ctx, key, strings.NewReader("primero")); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	if _, err := s.Put(ctx, key, strings.NewReader("segundo")); err != nil {
		t.Fatalf("Put 2: %v", err)
	}

	rc, _, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "segundo" {
		t.Errorf("cuerpo = %q, quiero \"segundo\" (la reescritura es idempotente)", body)
	}
}

func TestGetInexistente(t *testing.T) {
	s := mustFS(t)
	_, _, err := s.Get(context.Background(), "no/existe.md")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, quiero ErrNotFound", err)
	}
}

func TestHeadYDelete(t *testing.T) {
	s := mustFS(t)
	ctx := context.Background()
	const key = "bundles/u1/b1/log.md"

	if _, err := s.Put(ctx, key, strings.NewReader("registro")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.Head(ctx, key); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Head(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("Head tras Delete = %v, quiero ErrNotFound", err)
	}
	if err := s.Delete(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete de nuevo = %v, quiero ErrNotFound", err)
	}
}

func TestListPorPrefijo(t *testing.T) {
	s := mustFS(t)
	ctx := context.Background()
	claves := []string{
		"bundles/u1/b1/index.md",
		"bundles/u1/b1/capitulo-01.md",
		"bundles/u2/b9/index.md", // de otro usuario: no debe salir con el prefijo de u1
		"originals/u1/doc",
	}
	for _, k := range claves {
		if _, err := s.Put(ctx, k, strings.NewReader("x")); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	got, err := s.List(ctx, "bundles/u1/b1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List devolvió %d objetos, quiero 2: %+v", len(got), got)
	}
	for _, o := range got {
		if !strings.HasPrefix(o.Key, "bundles/u1/b1") {
			t.Errorf("clave fuera de prefijo: %q", o.Key)
		}
	}
}

// TestRechazaTraversal es el test de seguridad: ninguna clave puede escapar de la
// raíz. Es una tabla de casos, patrón idiomático en Go para probar muchas entradas
// con un solo bloque.
func TestRechazaTraversal(t *testing.T) {
	s := mustFS(t)
	ctx := context.Background()
	malas := []string{
		"",
		"/etc/passwd",
		"../fuera.md",
		"bundles/../../fuera.md",
		"bundles/./index.md",
		"a\\b.md",
	}
	for _, k := range malas {
		t.Run(k, func(t *testing.T) {
			if _, err := s.Put(ctx, k, strings.NewReader("x")); !errors.Is(err, ErrInvalidKey) {
				t.Errorf("Put(%q) = %v, quiero ErrInvalidKey", k, err)
			}
		})
	}
}
