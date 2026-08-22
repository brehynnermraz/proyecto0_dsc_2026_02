// Command seed hace el papel de la API para poder probar el worker sin la otra
// aplicación.
//
// PASO 2 (con base de datos): por cada .html de -dir hace lo mismo que haría la API:
//  1. sube el original al object store bajo originals/{owner}/{doc};
//  2. en UNA transacción, inserta el usuario (si falta), el documento y el job
//     (status='queued'), y hace COMMIT;
//  3. publica en la cola un mensaje diminuto con solo el job_id.
//
// El worker relee del paso 2 todo lo demás (storage_key, mime, filename) de la base de
// datos al tomar el trabajo. seed y worker comparten DATABASE_URL y el object store.
//
//	go run ./cmd/seed                 # sube+inserta+publica todos los ./testdata/mocks/*.html
//	go run ./cmd/seed -dir otra/carpeta
//	go run ./cmd/seed -n 5            # repite el lote 5 veces (demo de escalado)
//	go run ./cmd/seed -owner <uuid>   # reutiliza un owner concreto
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniandes-isis4426/okf/worker/internal/adapter/amqp"
	"github.com/uniandes-isis4426/okf/worker/internal/adapter/objectstore"
	"github.com/uniandes-isis4426/okf/worker/internal/config"
)

func main() {
	var (
		dir    = flag.String("dir", "./testdata/mocks", "carpeta con los .html a publicar")
		owner  = flag.String("owner", "", "owner_id; si se omite se genera uno")
		rounds = flag.Int("n", 1, "cuántas veces publicar el lote completo")
	)
	flag.Parse()

	if err := run(*dir, *owner, *rounds); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(dir, owner string, rounds int) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	var files []string
	for _, pat := range []string{"*.html", "*.epub"} {
		m, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			return fmt.Errorf("buscando %s en %s: %w", pat, dir, err)
		}
		files = append(files, m...)
	}
	if len(files) == 0 {
		return fmt.Errorf("no hay archivos .html ni .epub en %s", dir)
	}

	ownerID, err := parseOrNew(owner)
	if err != nil {
		return fmt.Errorf("owner inválido: %w", err)
	}

	ctx := context.Background()

	// Mismo store que usa el worker (carpeta con fs, servicio con http).
	store, err := objectstore.New(cfg)
	if err != nil {
		return err
	}

	// Misma base de datos que el worker: aquí insertamos las filas que él releerá.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("conectando a Postgres: %w", err)
	}
	defer pool.Close()

	pub, err := amqp.NewPublisher(cfg)
	if err != nil {
		return err
	}
	defer pub.Close()

	// El usuario dueño de los documentos tiene que existir por la FK. Se crea una vez.
	if err := ensureUser(ctx, pool, ownerID); err != nil {
		return fmt.Errorf("creando owner %s: %w", ownerID, err)
	}

	total := 0
	for r := 0; r < rounds; r++ {
		for _, path := range files {
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("leyendo %s: %w", path, err)
			}

			jobID := uuid.New()
			docID := uuid.New()
			key := fmt.Sprintf("originals/%s/%s", ownerID, docID)
			mime := mimeByExt(path)

			opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)

			// 1. Subir el original al store (como haría la API al recibir la carga).
			if err := store.Put(opCtx, key, bytes.NewReader(content), int64(len(content)), mime); err != nil {
				cancel()
				return fmt.Errorf("subiendo %s al store: %w", path, err)
			}

			// 2. Insertar documento + job (queued) en una transacción y COMMIT.
			if err := insertDocumentAndJob(opCtx, pool, insertParams{
				JobID:    jobID,
				DocID:    docID,
				OwnerID:  ownerID,
				Filename: filepath.Base(path),
				MIME:     mime,
				Size:     int64(len(content)),
				Key:      key,
			}); err != nil {
				cancel()
				return fmt.Errorf("insertando doc/job de %s: %w", path, err)
			}

			// 3. Publicar solo el job_id (el resto lo relee el worker de la BD).
			err = pub.Publish(opCtx, amqp.JobMessage{
				JobID:      jobID,
				DocumentID: docID,
				OwnerID:    ownerID,
			})
			cancel()
			if err != nil {
				return fmt.Errorf("publicando %s: %w", path, err)
			}

			total++
			fmt.Printf("subido+insertado+publicado job_id=%s doc=%s archivo=%s (%d bytes)\n",
				jobID, docID, filepath.Base(path), len(content))
		}
	}

	fmt.Printf("listo: %d trabajos encolados (owner=%s)\n", total, ownerID)
	return nil
}

// ensureUser crea la fila de users si no existe (idempotente por id).
func ensureUser(ctx context.Context, pool *pgxpool.Pool, ownerID uuid.UUID) error {
	email := fmt.Sprintf("seed-%s@example.com", ownerID)
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')
		 ON CONFLICT (id) DO NOTHING`, ownerID, email)
	return err
}

type insertParams struct {
	JobID, DocID, OwnerID uuid.UUID
	Filename, MIME, Key   string
	Size                  int64
}

// insertDocumentAndJob escribe el documento y su job (queued) en una transacción: o
// están las dos filas o ninguna, igual que haría la API antes de publicar.
func insertDocumentAndJob(ctx context.Context, pool *pgxpool.Pool, p insertParams) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO documents (id, owner_id, filename, mime, size_bytes, storage_key)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		p.DocID, p.OwnerID, p.Filename, p.MIME, p.Size, p.Key); err != nil {
		return fmt.Errorf("insert documents: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO jobs (id, document_id, owner_id, status) VALUES ($1, $2, $3, 'queued')`,
		p.JobID, p.DocID, p.OwnerID); err != nil {
		return fmt.Errorf("insert jobs: %w", err)
	}

	return tx.Commit(ctx)
}

// mimeByExt deduce el MIME por la extensión del archivo (aquí, en el productor). El
// worker NO lo deduce: lo relee de la base de datos.
func mimeByExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".epub":
		return "application/epub+zip"
	default:
		return "text/html"
	}
}

func parseOrNew(s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.New(), nil
	}
	return uuid.Parse(s)
}
