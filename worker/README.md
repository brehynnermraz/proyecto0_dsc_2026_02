# worker

Aplicación Go que consume trabajos de RabbitMQ y (a partir del paso 3) los convierte
en bundles OKF. Arquitectura hexagonal: la regla de dependencia apunta hacia dentro.

## Estado por pieza (PASO 1)

✅ = implementado y corre · 📝 = esqueleto con la descripción de lo que irá ahí

| Pieza | Archivo | Estado |
|---|---|---|
| Configuración desde el entorno | `internal/config/config.go` | ✅ (solo AMQP + ciclo de vida) |
| Logger estructurado | `internal/logging/logging.go` | ✅ |
| Topología de RabbitMQ (exchanges, colas, DLX, retry) | `internal/adapter/amqp/topology.go` | ✅ |
| Contrato del mensaje | `internal/adapter/amqp/message.go` | ✅ |
| Publicador | `internal/adapter/amqp/publisher.go` | ✅ |
| Receptor: consumo, ack/nack, reconexión, apagado | `internal/adapter/amqp/consumer.go` | ✅ |
| Puerto JobProcessor | `internal/port/port.go` | ✅ |
| Errores centinela | `internal/job/errors.go` | ✅ |
| Procesador **simulado** (log + sleep) | `internal/job/processor.go` | ✅ |
| Composition root + `/healthz` `/readyz` | `cmd/worker/main.go` | ✅ |
| Publicador de prueba | `cmd/seed/main.go` | ✅ |
| Repositorio de Postgres | `internal/adapter/db/repo.go` | 📝 paso 2 |
| Conversor (parsers, segmenter, renderer, validator) | `internal/converter/**` | 📝 paso 3 |
| Almacenamiento (fs, MinIO) | `internal/adapter/objectstore/*` | 📝 paso 3 |
| Notificación a la API | `internal/adapter/apiclient/client.go` | 📝 paso 3 |
| Tipos del dominio | `internal/domain/*` | 📝 paso 2 |

## Cómo probarlo (paso 1)

Requiere un RabbitMQ. Docker completo llega en el paso 4; por ahora, uno suelto:

```bash
docker run -d --name rabbit -p 5672:5672 -p 15672:15672 rabbitmq:3-management
# UI de gestión: http://localhost:15672  (guest / guest)

export AMQP_URL=amqp://guest:guest@localhost:5672/

# comprobar que compila y el formato está bien
go build ./... && go vet ./... && gofmt -l .

# terminal 1: el worker
go run ./cmd/worker

# terminal 2: publicar 3 trabajos
go run ./cmd/seed -n 3
```

El worker imprime cada `job_id`, "trabaja" 2 s (simulado) y hace `ack`. En la UI, la
cola `jobs.convert` se vacía.

- **Reparto por capacidad:** arranca 2-3 `go run ./cmd/worker` y publica `-n 9`.
- **Sala de espera (retry/DLX):** arranca el worker con `SIMULATE_FAILURE=true`; el
  mensaje hace `nack`, cae en `jobs.retry` y vuelve a `jobs.convert` a los 30 s.
