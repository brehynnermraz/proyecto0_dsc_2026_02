-- Notifica en el canal "job_status" cada vez que un job cambia de estado,
-- sin importar qué proceso hizo el UPDATE (worker, reintentos futuros, un
-- job manual desde psql). Evita que la API dependa de que cada call site en
-- Go se acuerde de emitir el evento.
CREATE OR REPLACE FUNCTION notify_job_status() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('job_status', NEW.id::text || ':' || NEW.status);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER jobs_notify_status
AFTER UPDATE OF status ON jobs
FOR EACH ROW
WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION notify_job_status();
