# Plataforma de conversión documental a bundles OKF

Proyecto de nivelación — **ISIS4426 Desarrollo de Soluciones Cloud** (2026‑2).

Plataforma web **multiusuario** que recibe documentos, los procesa **de forma
asíncrona mediante workers** y produce un **bundle de conocimiento compatible con
Open Knowledge Format (OKF)**. El backend (API y workers) está en **Go** y todo el
sistema **se despliega con un solo comando de Docker Compose**. La conversión
**nunca** ocurre dentro de la petición HTTP de carga.

> Flujo: el usuario carga un documento → la API responde **de inmediato** con un
> `job_id` → un worker lo convierte en segundo plano → el usuario consulta el
> estado y descarga el bundle validado.

---

## Arquitectura

Seis servicios, orquestados por Docker Compose, cada uno con una sola responsabilidad:

| Servicio           | Rol                                                                 | Tecnología        |
|--------------------|---------------------------------------------------------------------|-------------------|
| `frontend`         | Interfaz: carga, seguimiento del estado (SSE) y descarga            | Next.js           |
| `api`              | Recibe peticiones, publica el trabajo y retorna. **Sin estado**     | Go + Gin          |
| `worker` (×N)      | Consume la cola, convierte y publica el bundle. **Escalable**       | Go                |
| `postgres`         | **Fuente de verdad** del estado (usuarios, documentos, jobs, bundles)| PostgreSQL       |
| `rabbitmq`         | Cola de mensajes que desacopla la API de los workers                | RabbitMQ          |
| `object-storage`   | Guarda originales y bundles **fuera del disco efímero**             | Go (servicio HTTP)|

Reglas de arquitectura (todas exigidas por el enunciado, §4):

- **La API no mantiene estado**: no guarda trabajos ni archivos en memoria ni en el
  disco del contenedor. Solo publica en la cola y consulta/escribe en Postgres.
- **La conversión corre en workers independientes**, escalables sin tocar la API
  (`docker compose up --scale worker=3`).
- **La entrega es por cola**: la API publica solo el `job_id` y responde ya.
- **Originales y bundles viven en object‑storage** (volumen persistente), no en el
  disco efímero de los contenedores.
- **Todos los metadatos en Postgres** (volumen persistente).

El estado llega al navegador en vivo por el camino **Postgres → `NOTIFY` → SSE**,
sin polling.

---

## Requisitos

- **Docker** y **Docker Compose v2** (`docker compose ...`). Nada más: Go, Node y
  las migraciones corren dentro de los contenedores.
- Para las pruebas por línea de comandos (opcional): `curl`, `jq`, `unzip`.

---

## Despliegue con un solo comando

Desde esta carpeta (`task_learning_01/`):

```bash
docker compose up --build -d
```

Eso levanta los **6 servicios**, aplica **solas** las migraciones de la base
(`migrations/0001_init.sql` y `0002_job_status_notify.sql`, montadas en el initdb
de Postgres) y deja el sistema operativo. **No hace falta crear ningún `.env`**:
los secretos traen un valor por defecto para local (ver [Configuración](#configuración)).

Comprobar que todo quedó arriba:

```bash
docker compose ps                     # los 6 servicios: healthy / running
curl -s localhost:8080/healthz        # API            -> 200
curl -s localhost:9000/healthz        # object-storage -> {"status":"ok"}
```

Abrir la aplicación: **http://localhost:3000**

### Puertos publicados en el host

Remapeados para no chocar con Postgres/RabbitMQ **nativos** (dentro de Compose los
servicios se hablan por nombre, p. ej. `postgres:5432`):

| Servicio          | Host              | Dentro de Compose      |
|-------------------|-------------------|------------------------|
| frontend          | `3000`            | `frontend:3000`        |
| API               | `8080`            | `api:8080`             |
| object-storage    | `9000`            | `object-storage:9000`  |
| Postgres          | **`5433`** → 5432 | `postgres:5432`        |
| RabbitMQ (AMQP)   | **`5673`** → 5672 | `rabbitmq:5672`        |
| RabbitMQ (UI)     | **`15673`** → 15672 | (guest/guest)        |

### Escalar los workers

```bash
docker compose up -d --scale worker=3     # 3 réplicas compitiendo por la cola
```

### Parar y reiniciar

```bash
docker compose down        # para todo, CONSERVA los datos (volúmenes)
docker compose down -v     # además borra los volúmenes -> re-aplica migraciones al re-levantar
```

> Las migraciones solo corren cuando el volumen `pgdata` está vacío (primer init).
> Si editas una migración, usa `docker compose down -v` para que se vuelvan a aplicar.

---

## Configuración

Las variables de configuración están **separadas del código** (requisito §7). El
`docker-compose.yml` las inyecta a cada servicio; los secretos traen un valor por
defecto (`cambia-esto`) para que `docker compose up` funcione tal cual en local.

Para usar valores propios, sin tocar el compose, cualquiera de estas formas:

```bash
# a) un .env en esta carpeta (Compose lo lee solo; está en .gitignore)
cp .env.example .env        # y edita STORAGE_TOKEN / JWT_SECRET / ...

# b) exportándolos solo para este arranque
STORAGE_TOKEN=mi-token JWT_SECRET=mi-secreto docker compose up --build -d

# c) apuntando Compose a la plantilla directamente
docker compose --env-file .env.example up --build -d
```

Variables principales (ver `.env.example` para la lista completa):

| Variable            | Para qué                                                        | Default    |
|---------------------|----------------------------------------------------------------|------------|
| `POSTGRES_USER/PASSWORD/DB` | Credenciales y nombre de la base                       | `okf`      |
| `RABBITMQ_USER/PASSWORD`    | Credenciales del broker                                | `guest`    |
| `STORAGE_TOKEN`     | Secreto **compartido** por api, worker y object-storage         | `cambia-esto` |
| `JWT_SECRET`        | Firma de los tokens de sesión (HMAC)                            | `cambia-esto` |
| `FRONTEND_ORIGIN`   | Origen permitido por CORS en la API                             | `http://localhost:3000` |
| `NEXT_PUBLIC_API_URL` | URL de la API que usa el navegador (se incrusta al *build*)   | `http://localhost:8080` |
| `PROCESSING_DELAY`  | Retardo artificial en el worker (solo para la demo de asincronía)| `0s`       |

> `STORAGE_TOKEN` debe ser el **mismo** en los tres servicios que lo usan; si no
> coincide, object-storage responde `401` y los jobs fallan al leer/escribir.

---

## Probar el sistema

### A. Por la interfaz (camino feliz)

1. Abre http://localhost:3000, **regístrate** e inicia sesión.
2. En "Cargar documento" elige **HTML** o **EPUB** y sube uno de los mocks del repo:
   - `worker/testdata/mocks/introduccion-cloud.html`
   - `worker/testdata/mocks/guia-nube.epub`
3. En la tabla el estado pasa **En espera → Procesando → Completado** en vivo (SSE).
4. Al llegar a *Completado*, **Descargar bundle**: el `.zip` trae `index.md`,
   `log.md` y los conceptos (`NN-slug.md`).

### B. Por `curl` (sin frontend)

```bash
API=http://localhost:8080

# registro + login
curl -sS -X POST $API/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"demo@okf.test","password":"secret123"}'
TOKEN=$(curl -sS -X POST $API/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"demo@okf.test","password":"secret123"}' | jq -r .token)

# cargar un HTML -> retorna {"job_id":"...","status":"pending"} de inmediato
JOB=$(curl -sS -X POST $API/documents -H "Authorization: Bearer $TOKEN" \
  -F format=html -F file=@worker/testdata/mocks/introduccion-cloud.html | jq -r .job_id)

# seguir el estado en vivo (SSE); Ctrl+C al llegar a done
curl -N $API/jobs/$JOB/events -H "Authorization: Bearer $TOKEN"

# descargar el bundle
BUNDLE=$(curl -sS $API/jobs/$JOB -H "Authorization: Bearer $TOKEN" | jq -r .BundleID)
curl -sS -o bundle.zip $API/bundles/$BUNDLE/download -H "Authorization: Bearer $TOKEN"
unzip -l bundle.zip
```

### Endpoints de la API

| Método | Ruta                    | Descripción                                  |
|--------|-------------------------|----------------------------------------------|
| POST   | `/auth/register`        | Crear usuario                                |
| POST   | `/auth/login`           | Iniciar sesión → `{ token }`                 |
| POST   | `/documents`            | Cargar documento (form `format`, `file`) → `job_id` |
| GET    | `/jobs`                 | Listar **mis** trabajos                      |
| GET    | `/jobs/:id`             | Estado de un trabajo                         |
| GET    | `/jobs/:id/events`      | Estado en vivo (SSE)                         |
| DELETE | `/jobs/:id`             | Borrar un trabajo propio                     |
| GET    | `/bundles/:id/download` | Descargar el bundle (`.zip`)                 |

Todas las rutas de `/jobs` y `/bundles` exigen `Authorization: Bearer <token>` y
filtran por propietario: un recurso ajeno responde **404** sin revelar si existe.

---

## Demostrar las condiciones verificables (§6 del enunciado)

- **Asincronía efectiva** — enciende el retardo y observa la respuesta inmediata
  más la continuación tras cerrar la conexión:
  ```bash
  PROCESSING_DELAY=30s docker compose up -d worker
  # sube un documento: el job_id llega ya; el estado queda 'processing' ~30 s;
  # cierra la pestaña y vuelve: el trabajo terminó igual.
  PROCESSING_DELAY=0s  docker compose up -d worker     # apágalo al terminar
  ```
- **Documento breve** — sube `worker/testdata/mocks/sin-titulo.html`: produce
  `index.md`, `log.md` y **un único** concepto, sin fallar ni advertir.
- **Documento estructurado** — un HTML/EPUB con varias secciones produce un
  concepto por unidad, enlazados en orden desde `index.md`.
- **Bundle incompleto** — un archivo corrupto (p. ej. un `.epub` inválido) deja el
  job en `failed` y **no** publica bundle: la descarga responde 404.
- **Aislamiento** — B intenta el job/bundle de A (cada 404 con su control 200 de A):
  ```bash
  # A (dueño) y B (intruso)
  curl -sS -X POST $API/auth/register -H 'Content-Type: application/json' -d '{"email":"a@okf.test","password":"secret123"}' >/dev/null 2>&1
  TA=$(curl -sS -X POST $API/auth/login -H 'Content-Type: application/json' -d '{"email":"a@okf.test","password":"secret123"}' | jq -r .token)
  curl -sS -X POST $API/auth/register -H 'Content-Type: application/json' -d '{"email":"b@okf.test","password":"secret123"}' >/dev/null 2>&1
  TB=$(curl -sS -X POST $API/auth/login -H 'Content-Type: application/json' -d '{"email":"b@okf.test","password":"secret123"}' | jq -r .token)
  JA=$(curl -sS -X POST $API/documents -H "Authorization: Bearer $TA" -F format=html -F file=@worker/testdata/mocks/introduccion-cloud.html | jq -r .job_id)
  until [ "$(curl -sS $API/jobs/$JA -H "Authorization: Bearer $TA" | jq -r .Status)" = done ]; do sleep 1; done
  echo "A lee su job   -> $(curl -sS -o /dev/null -w '%{http_code}' $API/jobs/$JA -H "Authorization: Bearer $TA")  (200)"
  echo "B lee job de A -> $(curl -sS -o /dev/null -w '%{http_code}' $API/jobs/$JA -H "Authorization: Bearer $TB")  (404)"
  ```
- **Ausencia de duplicados (idempotencia)** — con un job en `processing`, reinyecta
  el MISMO mensaje desde la UI de RabbitMQ (http://localhost:15673 → Exchanges →
  `okf.jobs` → Publish, routing key `convert`, `message_id` = el `job_id`, payload
  `{"job_id":"<job_id>"}`). El worker lo descarta (`Claim` condicional) y sigue
  habiendo **un solo** bundle.

---

## Estructura del bundle OKF

Cada conversión produce una carpeta autocontenida (§3 del enunciado):

```
bundle/
├── index.md        # navegación y datos del bundle; enlaza los conceptos en orden
├── log.md          # trazabilidad: unidades detectadas, transformaciones, validaciones
└── NN-concepto.md  # uno por unidad lógica (al menos uno, incluso para un doc breve)
```

Antes de publicar, el worker **valida** la estructura mínima y la resolución de los
enlaces del índice; si no pasa, no se publica ni se habilita la descarga.

---

## Estructura del repositorio

```
task_learning_01/
├── docker-compose.yml     # orquesta los 6 servicios (un solo comando)
├── .env.example           # variables de configuración (separadas del código)
├── migrations/            # esquema + trigger de NOTIFY (se aplican solas en Postgres)
├── frontend/              # Next.js (Dockerfile, salida standalone)
├── backend/               # API en Go (Gin) — Dockerfile.api
├── worker/                # worker en Go (conversor, arquitectura hexagonal)
│   └── testdata/mocks/    # documentos de ejemplo para las pruebas
└── object-storage/        # servicio de almacenamiento de objetos (Go)
```

---

## Problemas comunes

| Síntoma | Causa probable |
|---|---|
| `401` desde object-storage | `STORAGE_TOKEN` distinto entre api, worker y object-storage |
| Jobs atascados en `queued` | worker caído, o falta `docker compose up -d worker` tras un cambio |
| El SSE nunca llega a *done* | no se aplicó `migrations/0002_...` (usa `docker compose down -v` y re-levanta) |
| El build da `403` del proxy de Go | rate-limit de `proxy.golang.org`; reintenta `docker compose build` |
| Un cambio de código "no aparece" | reconstruye la imagen: `docker compose up -d --build <servicio>` |

---

## Declaración de uso de IA

Durante el desarrollo se utilizó **Claude (Anthropic)** como herramienta de
asistencia (generación y refactorización de código, análisis de errores y apoyo en
las pruebas). Las decisiones de arquitectura, la implementación y la validación
finales fueron del equipo, que mantiene la responsabilidad sobre el resultado.
