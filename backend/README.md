# Backend — plataforma de conversión documental a bundles OKF

Go (Gin) siguiendo arquitectura hexagonal: dos binarios (`cmd/api`,
`cmd/worker`) que comparten el mismo dominio y los mismos puertos, con
adaptadores distintos para cada uno. La API nunca convierte documentos
dentro de la petición HTTP — solo encola el trabajo en RabbitMQ y responde
de inmediato; el worker es quien procesa en segundo plano.

## Estructura

```
cmd/
  api/main.go           entrypoint de la API HTTP
  worker/main.go         entrypoint del worker (worker x N, prefetch=1 cada uno)
internal/
  domain/                 entidades + reglas puras: Document, Job, Bundle, Concept
  ports/                   interfaces que el dominio necesita del exterior
  app/
    submit_document.go      caso de uso: recibe el documento, lo guarda y encola el job
    process_job.go           caso de uso: el worker convierte y publica el bundle
    delete_job.go             caso de uso: borra un job y todo lo que le pertenece
  adapters/
    http/
      router.go               registro de rutas
      handlers/                 auth, documents, jobs, bundles
      middleware/                JWT, CORS, secreto compartido del webhook
    worker/                  consumer RabbitMQ con ack manual
    queue/                    topología RabbitMQ (jobs / jobs.retry / jobs.dead)
    storage/                  cliente MinIO (incluye zip en streaming para la descarga)
    db/                        repositorios Postgres + migrations/, listener LISTEN/NOTIFY
    events/                    hub en memoria que reparte eventos SSE por job_id
    converter/                  Markdown (real), texto plano (trivial), HTML (stub, ver abajo)
    auth/                      emisor/verificador JWT
  config/                   carga de variables de entorno
```

## Cómo arrancar

**Con Docker Compose** (recomendado, ver el `docker-compose.yml` en la raíz
del repo): un solo `docker compose up --build` desde la raíz levanta
Postgres, RabbitMQ, MinIO, la API y el worker, aplicando las migraciones
automáticamente al iniciar Postgres.

**Localmente**, requiere Go 1.26+ y Postgres/RabbitMQ/MinIO corriendo (por
ejemplo, arrancando solo esos tres servicios con Docker Compose). No hay
`go.sum` comprometido a propósito — el `Dockerfile.api`/`Dockerfile.worker`
corren `go mod tidy` en el build; para correr fuera de Docker, hacerlo una
vez a mano:

```
go mod tidy
```

Aplicar `internal/adapters/db/migrations/*.sql` (en orden) contra Postgres
antes del primer arranque si no se usa Docker Compose. Luego:

```
go run ./cmd/api
go run ./cmd/worker
```

## Variables de entorno

| Variable | Default | Uso |
|---|---|---|
| `PORT` | `8080` | puerto de la API |
| `POSTGRES_DSN` | `postgres://okf:okf@postgres:5432/okf?sslmode=disable` | conexión a Postgres |
| `RABBITMQ_URL` | `amqp://guest:guest@rabbitmq:5672/` | conexión a RabbitMQ |
| `MINIO_ENDPOINT` | `minio:9000` | host:puerto de MinIO |
| `MINIO_ACCESS_KEY` / `MINIO_SECRET_KEY` | `minioadmin` | credenciales MinIO |
| `MINIO_BUCKET` | `okf` | bucket donde viven originales y bundles |
| `MINIO_USE_SSL` | `false` | si la conexión a MinIO usa TLS |
| `JWT_SECRET` | *(vacío — definirlo siempre)* | clave HMAC para firmar los JWT de usuario |
| `WORKER_WEBHOOK_SECRET` | *(vacío — definirlo siempre)* | secreto compartido que autentica al worker en `POST /webhooks/jobs/:id/status` |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | único origen habilitado por CORS para llamar a la API desde el navegador |
| `WORKER_CONCURRENCY` | `3` | cuántos consumers arranca `cmd/worker` |

Todas se cargan en `internal/config/config.go` y se pasan por variables de
entorno (ver `docker-compose.yml` y `.env.example` en la raíz) — nunca están
hardcodeadas en el código.

## Endpoints de la API

| Método | Ruta | Auth | Descripción |
|---|---|---|---|
| `GET` | `/healthz` | — | liveness check |
| `POST` | `/auth/register` | — | crea un usuario (`email`, `password`) |
| `POST` | `/auth/login` | — | devuelve un JWT |
| `POST` | `/documents` | JWT | sube un documento (`multipart/form-data`: `file`, `format`), encola el job y responde `202` con `job_id` de inmediato |
| `GET` | `/jobs/:id` | JWT | estado puntual del job |
| `GET` | `/jobs/:id/events` | JWT | el mismo estado, en vivo por Server-Sent Events |
| `DELETE` | `/jobs/:id` | JWT | borra el job junto con su documento original y su bundle (si existe), en la base de datos y en MinIO |
| `GET` | `/bundles/:id/download` | JWT | descarga el bundle como `.zip`, empaquetado en streaming |
| `POST` | `/webhooks/jobs/:id/status` | `X-Webhook-Secret` | el worker notifica que un job terminó (ver abajo) |

Los endpoints con JWT devuelven **404** (no 403) cuando el recurso
pertenece a otro usuario, para no revelar que existe (sección 6 del
enunciado, "aislamiento").

## Notificación de estado en tiempo real (SSE)

`GET /jobs/:id/events`, autenticado igual que `GET /jobs/:id`, empuja el
estado del job por Server-Sent Events en vez de que el frontend haga polling:

```
event: status
data: {"status":"processing"}
```

El mecanismo: un trigger de Postgres (`migrations/0002_job_status_notify.sql`)
hace `pg_notify` en cada cambio de `jobs.status`, sin importar qué código lo
causó. La API mantiene una conexión dedicada escuchando ese canal
(`internal/adapters/db/listener.go` — LISTEN/NOTIFY no puede vivir en el pool
normal) y reparte los eventos por un hub en memoria
(`internal/adapters/events/hub.go`) a quien esté suscrito a ese `job_id`. El
handler se suscribe *antes* de leer el estado actual para no perder un
cambio que ocurra justo en esa ventana, y cierra la conexión solo al llegar
a un estado terminal (`done`/`failed`).

Si corren varias réplicas de la API (fuera del alcance de este esqueleto),
cada una necesita su propio listener — no hace falta coordinación extra
porque Postgres manda la notificación a todos los que estén escuchando el
canal.

## Webhook del worker

`POST /webhooks/jobs/:id/status` es el endpoint que el worker llama para
notificar que terminó de procesar un job (éxito o falla). No usa JWT de
usuario — se autentica con el header `X-Webhook-Secret`, comparado en
tiempo constante contra `WORKER_WEBHOOK_SECRET` (ver
`internal/adapters/http/middleware/webhook.go`).

```json
// éxito
{ "status": "done", "bundle_id": "..." }

// falla
{ "status": "failed", "attempt": 2, "error": "mensaje" }
```

Internamente llama a las mismas `MarkDone`/`MarkFailed` que ya usa el flujo
actual del worker (que hoy escribe directo a Postgres vía
`ports.JobRepository`), así que el trigger de `pg_notify` y el SSE de arriba
siguen funcionando sin cambios.

## Borrado de trabajos

`DELETE /jobs/:id` (implementado en `internal/app/delete_job.go`) borra, en
este orden: el bundle en MinIO, la fila de `bundles`, la fila de `jobs` y
por último el original en MinIO y la fila de `documents`.

El orden importa por una FK circular entre `jobs` y `bundles`
(`jobs.bundle_id -> bundles.id` y `bundles.job_id -> jobs.id`, ver
`migrations/0001_init.sql`): antes de poder borrar el bundle hay que romper
la referencia con `JobRepository.ClearBundle` (`UPDATE jobs SET
bundle_id=NULL`), si no Postgres rechaza el borrado por violar la
restricción.

## Idempotencia y aislamiento

- `JobRepo.ClaimForProcessing` transiciona atómicamente un job de `pending`
  **o `failed`** a `processing`. Incluir `failed` es necesario para que un
  reintento con backoff (cola `jobs.retry`, ver `queue/rabbitmq.go`)
  realmente vuelva a ejecutar la conversión — si solo aceptara `pending`, un
  job que falló una vez quedaría atascado para siempre, porque la entrega
  reentregada encontraría `status="failed"` y el Consumer haría `Ack` sin
  reprocesar nada.
- Un job en `processing` queda fuera de ese `UPDATE`, así una entrega
  concurrente de la misma entrega en curso no se le adelanta (sección 6,
  "ausencia de duplicados").
- Los handlers de `jobs` y `bundles` devuelven **404** cuando el recurso
  pertenece a otro usuario, para no revelar que existe.

## Lo que falta a propósito (marcado con TODO en el código)

- **Conversor HTML** (`internal/adapters/converter/html.go`): stub que
  siempre devuelve error; registrar uno nuevo es solo implementar
  `ports.DocumentConverter` y añadirlo a la lista en `cmd/worker/main.go`.
  El frontend sí ofrece "HTML" como opción de formato, así que hoy ese
  camino termina en un job `failed` tras agotar los reintentos.
- **Extracción de `assets/`** y **cancelación de trabajos** (alcance
  opcional, sección 5.2 del enunciado): no están cableados todavía.
