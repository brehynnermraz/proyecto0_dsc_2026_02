-- Consultas del worker sobre la tabla jobs.
--
-- NOTA (paso 2): en este proyecto de aprendizaje NO usamos sqlc. El SQL real, tipado a
-- mano con pgx, vive en ../repo.go; este archivo queda como documentación del contrato.
-- Si algún día se adopta sqlc, estas mismas consultas se generarían desde aquí.

-- Claim toma el trabajo en exclusiva. Es la consulta más importante del worker.
--
--   · status IN ('queued','failed')  → IDEMPOTENCIA: la reentrega duplicada encuentra
--     cero filas y el segundo worker se retira.
--   · lease vencido en 'processing'   → RECUPERACIÓN: un worker muerto dejó el trabajo
--     en 'processing'; al vencer su lease, otro lo re-toma.
--
-- La condición viaja DENTRO del WHERE (nunca se comprueba en Go: sería una carrera).
-- Cero filas ⇒ job.ErrNotClaimable ⇒ ack. attempts se incrementa aquí; el procesador
-- decide 'dead' cuando attempts supera max_attempts.
WITH claimed AS (
    UPDATE jobs
       SET status       = 'processing',
           started_at   = now(),
           heartbeat_at = now(),
           attempts     = attempts + 1
     WHERE id = $1
       AND ( status IN ('queued','failed')
             OR (status = 'processing'
                 AND heartbeat_at < now() - make_interval(secs => $2::int)) )
    RETURNING id, document_id, owner_id, attempts, max_attempts, started_at
)
SELECT c.id, c.owner_id, c.attempts, c.max_attempts, c.started_at,
       d.id, d.filename, d.mime, d.size_bytes, d.storage_key
  FROM claimed c
  JOIN documents d ON d.id = c.document_id;

-- Heartbeat renueva el lease y hace de canal de cancelación. Cero filas ⇒ el trabajo
-- dejó de estar en 'processing' (cancelado o terminado) ⇒ abortar la conversión.
UPDATE jobs SET heartbeat_at = now() WHERE id = $1 AND status = 'processing';

-- Marcar succeeded (dentro de la transacción de Publish).
UPDATE jobs SET status = 'succeeded', finished_at = now(), error = NULL WHERE id = $1;

-- MarkFailed deja el trabajo reintentable: vuelve a 'failed', que Claim acepta.
UPDATE jobs SET status = 'failed', error = $2 WHERE id = $1;

-- MarkDead es terminal: agotó max_attempts y no se reintenta más.
UPDATE jobs SET status = 'dead', error = $2, finished_at = now() WHERE id = $1;
