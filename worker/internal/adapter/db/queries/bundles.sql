-- Publicación del bundle. Documentación del contrato; el SQL real está en ../repo.go.
--
-- bundles.job_id es UNIQUE. Junto con el ON CONFLICT, garantiza "a lo sumo un bundle
-- publicado por trabajo" aunque dos workers llegaran al final a la vez.
--
-- InsertBundle y el UPDATE ... succeeded se ejecutan SIEMPRE en la misma transacción.
-- El COMMIT es la publicación, no la escritura de archivos en el store: si la fila no
-- se inserta, la descarga responde 404. Cero filas (ON CONFLICT) ⇒ job.ErrAlreadyPublished.
INSERT INTO bundles (id, job_id, owner_id, storage_prefix, validation,
                     warnings, concept_count, size_bytes, published_at)
VALUES ($1, $2, $3, $4, $5::validation_status, $6::jsonb, $7, $8, now())
ON CONFLICT (job_id) DO NOTHING
RETURNING id;
