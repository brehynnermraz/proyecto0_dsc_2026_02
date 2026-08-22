-- Notificación de cambios de estado para el SSE de la API.
--
-- La API (../backend) empuja al navegador el estado del job en vivo por
-- Server-Sent Events. La fuente de esos eventos es este trigger: cada vez que
-- CUALQUIER proceso cambia jobs.status (el worker al reclamar, al publicar el
-- bundle o al fallar; un cambio manual desde psql), Postgres emite un
-- pg_notify en el canal 'job_status' con "id:status".
--
-- Por qué un trigger y no que el worker llame a la API: el worker es la fuente
-- de verdad y ya escribe el estado en Postgres. Dejar que la base avise
-- desacopla al worker de la API (no necesita su URL ni un secreto) y no se
-- pierde ningún cambio, venga de donde venga el UPDATE.
--
-- La API mantiene una conexión LISTEN 'job_status'
-- (backend/internal/adapters/db/listener.go) y traduce el estado del worker
-- (queued/succeeded/...) al vocabulario del frontend (pending/done/...) antes
-- de emitirlo.
--
-- Idempotente: CREATE OR REPLACE + DROP TRIGGER IF EXISTS.
--
-- Aplicar:
--   psql -h localhost -U "$USER" -d okf_learning -f migrations/0002_job_status_notify.sql

CREATE OR REPLACE FUNCTION notify_job_status() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('job_status', NEW.id::text || ':' || NEW.status::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS jobs_notify_status ON jobs;

CREATE TRIGGER jobs_notify_status
AFTER UPDATE OF status ON jobs
FOR EACH ROW
WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION notify_job_status();
