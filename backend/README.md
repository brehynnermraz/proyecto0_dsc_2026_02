# Backend — plataforma de conversión documental a bundles OKF

Go (Gin) siguiendo arquitectura hexagonal. Un solo binario: `cmd/api`. La API
nunca convierte documentos dentro de la petición HTTP — solo encola el trabajo
en RabbitMQ y responde de inmediato; quien procesa en segundo plano es el
**worker**, que ya NO vive aquí sino en su propio módulo (`../worker`).

> Antes este backend traía su propio worker embebido (`cmd/worker`,
> `adapters/worker`, `app/process_job.go`, `adapters/converter/*`). Se eliminó
> por ser una duplicación del worker real de `../worker`: tener dos
> procesadores compitiendo por la misma cola no tiene sentido. La API conserva
> solo lo suyo (auth, carga, estado/SSE, descarga, borrado) y **publica el
> trabajo para que lo tome el worker de `../worker`**.

El contrato entre la API y el worker son **tres cosas y solo tres**:

1. **El mensaje de la cola** — `internal/ports/queue.go`, que coincide con
   `worker/internal/adapter/amqp/message.go` (`{job_id, document_id, owner_id}`
   con `MessageId=job_id`), publicado en la misma topología que declara el
   worker (`okf.jobs` + `okf.jobs.dlx`, `jobs.convert` quorum, retry, dead).
2. **El esquema de la base de datos** — es el del worker
   (`../migrations/`), la fuente de verdad compartida. La API lee/escribe
   contra ese esquema (documents.`mime`, enum `job_status`, `bundles.storage_prefix`).
3. **El object store** — el servicio `../object-storage`, el mismo almacén
   para los dos: la API sube el original ahí y el worker lo lee de ahí.

## Estructura

```
cmd/
  api/main.go           entrypoint de la API HTTP (único binario)
internal/
  domain/                 entidades + reglas puras: Document, Job, Bundle, User
  ports/                   interfaces que el dominio necesita del exterior
  app/
    submit_document.go      caso de uso: guarda el original, registra el job y lo encola
    delete_job.go             caso de uso: borra un job y todo lo que le pertenece
  adapters/
    http/
      router.go               registro de rutas
      handlers/                 auth, documents, jobs, bundles
      middleware/                JWT, CORS
    queue/                    publica en okf.jobs; declara la MISMA topología que el worker
    storage/                  cliente HTTP del servicio object-storage (zip en streaming para la descarga)
    db/                        repositorios Postgres (esquema del worker) + listener LISTEN/NOTIFY
    events/                    hub en memoria que reparte eventos SSE por job_id
    auth/                      emisor/verificador JWT
  config/                   carga de variables de entorno
```

La conversión (parsear, segmentar, renderizar, validar) vive en `../worker`, no
aquí: por eso no hay `adapters/converter/` ni `app/process_job.go`. El esquema
de la BD tampoco vive aquí: es `../migrations/` (propiedad conjunta).

## Cómo arrancar

Requiere Go 1.26+, y Postgres + RabbitMQ + el servicio `../object-storage`
corriendo. El worker (`../worker`) se arranca aparte, desde su propio módulo.

1. Aplicar el esquema compartido (`../migrations/*.sql`, en orden) contra
   Postgres — incluye `0002_job_status_notify.sql`, el trigger que alimenta el
   SSE. Ver `../migrations/README.md`.
2. Arrancar el servicio de objetos (`../object-storage`, ver su README).
3. Arrancar la API:

```
go run ./cmd/api
```

## Variables de entorno

| Variable | Default | Uso |
|---|---|---|
| `PORT` | `8080` | puerto de la API |
| `POSTGRES_DSN` | `postgres://okf:okf@postgres:5432/okf?sslmode=disable` | conexión a Postgres (esquema de `../migrations`) |
| `RABBITMQ_URL` | `amqp://guest:guest@rabbitmq:5672/` | conexión a RabbitMQ |
| `STORAGE_BASE_URL` | `http://object-storage:9000` | URL del servicio `../object-storage` (local: `http://localhost:9000`) |
| `STORAGE_TOKEN` | *(vacío — definirlo siempre)* | `Bearer` compartido con el servicio de objetos **y con el worker** |
| `JWT_SECRET` | *(vacío — definirlo siempre)* | clave HMAC para firmar los JWT de usuario |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | único origen habilitado por CORS |

`STORAGE_TOKEN` debe ser el mismo valor en la API, en el worker y en el servicio
de objetos, o las lecturas/escrituras darán `401`.

## Endpoints de la API

| Método | Ruta | Auth | Descripción |
|---|---|---|---|
| `GET` | `/healthz` | — | liveness check |
| `POST` | `/auth/register` | — | crea un usuario (`email`, `password`) |
| `POST` | `/auth/login` | — | devuelve un JWT |
| `POST` | `/documents` | JWT | sube un documento (`multipart/form-data`: `file`, `format` = `html`\|`epub`), encola el job y responde `202` con `job_id` |
| `GET` | `/jobs/:id` | JWT | estado puntual del job (Status ya traducido al vocabulario del frontend) |
| `GET` | `/jobs/:id/events` | JWT | el mismo estado, en vivo por Server-Sent Events |
| `DELETE` | `/jobs/:id` | JWT | borra el job junto con su documento original y su bundle (si existe), en Postgres y en el object store |
| `GET` | `/bundles/:id/download` | JWT | descarga el bundle como `.zip`, empaquetado en streaming |

Los endpoints con JWT devuelven **404** (no 403) cuando el recurso pertenece a
otro usuario, para no revelar que existe (sección 6, "aislamiento").

**Formatos**: solo `html` y `epub`, porque son los que el worker soporta hoy
(ver `worker/internal/converter/parser/registry.go`). El handler traduce el
`format` al MIME que el worker espera (`app.MIMEForFormat`) y rechaza con `400`
cualquier otro, en vez de crear un job que fallaría permanentemente.

## Estados: el worker manda, la API traduce

La base de datos usa el enum del worker (`queued/processing/succeeded/failed/
cancelled/dead`). El frontend solo entiende cuatro: `pending/processing/done/
failed`. La API traduce en la frontera (`domain.JobStatus.Frontend()`), tanto en
`GET /jobs/:id` (vía `Job.MarshalJSON`) como en el SSE:

| worker | frontend |
|---|---|
| `queued` | `pending` |
| `processing` | `processing` |
| `succeeded` | `done` |
| `failed` / `cancelled` / `dead` | `failed` |

Así el frontend nunca cambia y el worker sigue siendo la única fuente de verdad
del estado.

## Notificación de estado en tiempo real (SSE)

`GET /jobs/:id/events` empuja el estado del job por Server-Sent Events en vez de
polling:

```
event: status
data: {"status":"processing"}
```

El mecanismo, **sin webhook**: el worker escribe el estado directo en Postgres
(es la fuente de verdad); un trigger (`../migrations/0002_job_status_notify.sql`)
hace `pg_notify` en cada cambio de `jobs.status`, sin importar qué proceso lo
causó. La API mantiene una conexión dedicada escuchando ese canal
(`internal/adapters/db/listener.go` — LISTEN/NOTIFY no puede vivir en el pool
normal) y reparte los eventos por un hub en memoria
(`internal/adapters/events/hub.go`) a quien esté suscrito a ese `job_id`. El
handler se suscribe *antes* de leer el estado actual para no perder un cambio en
esa ventana, traduce el estado al vocabulario del frontend, y cierra la conexión
al llegar a un estado terminal.

Dejar que la base avise (en vez de que el worker llame a un webhook de la API)
desacopla al worker: no necesita conocer la URL ni un secreto de la API, y no se
pierde ningún cambio venga del proceso que venga. Por eso NO existe endpoint de
webhook. (El worker trae un notificador best-effort simulado en
`worker/internal/adapter/apiclient`; con este diseño no hace falta activarlo.)

## Borrado de trabajos

`DELETE /jobs/:id` (`internal/app/delete_job.go`): primero limpia los objetos del
store (el bundle bajo su prefijo y el original), y luego borra la fila de
`documents`. En el esquema del worker **no hay FK circular**: son
`jobs.document_id -> documents` y `bundles.job_id -> jobs`, ambas
`ON DELETE CASCADE`, así que borrar el documento arrastra el job y el bundle en
una sola sentencia. El object store no participa del CASCADE — por eso se limpia
a mano y antes.

## Idempotencia y aislamiento

- **Idempotencia**: es responsabilidad del worker, no de la API. El worker
  reclama el job con un `UPDATE` condicional (claim + lease) que solo gana una
  entrega; una entrega duplicada de RabbitMQ no produce un segundo bundle. La
  API solo publica el mensaje.
- **Aislamiento**: los handlers de `jobs` y `bundles` devuelven **404** cuando
  el recurso pertenece a otro usuario, para no revelar que existe.
