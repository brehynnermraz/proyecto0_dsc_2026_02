# Diagrama de la solución actual (paso 2)

Estado: flujo real de punta a punta **con base de datos, sin Docker**. **Postgres es la
fuente de verdad** del estado del trabajo; la cola solo AVISA que hay trabajo. El
archivo NO viaja por RabbitMQ (patrón *Claim Check*): va por el object store y por la
cola solo viaja el `job_id`.

Dos ideas centrales:

- **La cola avisa, la base de datos autoriza.** El worker no procesa el mensaje: toma
  el trabajo en Postgres (`Claim`, un UPDATE condicional) y de ahí relee la
  `storage_key`, el `mime` y el `filename`. Nada de eso viaja ya en el mensaje.
- **El object store** tiene dos backends intercambiables por `STORAGE_BACKEND`, ambos
  detrás del puerto `Store`: `fs` (carpeta local) y `http` (servicio `object-storage`
  por red, `GET`/`PUT` a `/v1/objects/{key}` con `Bearer`). Los diagramas muestran `http`.

---

## 1. Flujo de punta a punta

```mermaid
flowchart LR
    mocks["testdata/mocks/*.html"]

    subgraph SEED["cmd/seed (hace de API)"]
        s1["1. leer .html"]
        s2["2. store.Put(originals/owner/doc)"]
        s3["3. TX: INSERT documents + jobs(queued)"]
        s4["4. publicar {job_id}"]
    end

    subgraph BROKER["RabbitMQ"]
        q["cola jobs.convert (quorum)"]
    end

    subgraph WORKER["cmd/worker (consumidor)"]
        w1["5. Claim(job_id) -> doc"]
        w2["6. store.Get(storage_key) + LimitReader"]
        w3["7. converter.Convert (parse/segmentar/render/validar)"]
        w4["8. store.Put(bundles/owner/bundle/...)"]
        w5["9. Publish: TX bundle + job=succeeded"]
        w6["10. ack"]
    end

    subgraph DB[("Postgres (fuente de verdad)")]
        tj["jobs / documents / bundles"]
    end

    subgraph STORE["Object store (fs | servicio http)"]
        orig["originals/owner/doc  (bruto)"]
        bund["bundles/owner/bundle/*.md (procesado)"]
    end

    mocks --> s1 --> s2 --> s3 --> s4
    s2 -. "PUT bytes" .-> orig
    s3 -. "INSERT" .-> tj
    s4 -- "job_id" --> q
    q --> w1 --> w2 --> w3 --> w4 --> w5 --> w6
    w1 -. "UPDATE ... processing" .-> tj
    w2 -. "GET" .-> orig
    w4 -. "PUT" .-> bund
    w5 -. "COMMIT" .-> tj

    classDef store fill:#eef,stroke:#88a
    classDef db fill:#fde,stroke:#a68
    class orig,bund store
    class tj db
```

Clave: por la cola (`s4 -> q`) solo va `{job_id}`. Los **bytes** van por el object
store (flechas a `originals`/`bundles`) y el **estado** por Postgres. El worker relee
de la BD todo lo que necesita al hacer `Claim`.

---

## 2. Topología de RabbitMQ (colas, reintento y DLX)

Declarada e idempotente en `amqp/topology.go`. Los reintentos usan una "sala de espera"
con TTL, sin consumidores, para conseguir backoff gratis.

```mermaid
flowchart TB
    pub["publisher (seed)"] -->|"routing key: convert"| ex["exchange okf.jobs (direct)"]
    ex --> convert["cola jobs.convert (quorum)"]
    convert -->|"consume"| worker["worker"]

    worker -->|"ack (ok / no-reclamable / cancelado / agotado / permanente)"| done["fin"]
    worker -->|"nack requeue=false (transitorio)"| dlx["exchange okf.jobs.dlx"]

    dlx --> retry["cola jobs.retry (x-message-ttl=30s, sin consumidores)"]
    retry -->|"al vencer el TTL, dead-letter"| ex

    convert -. "supera delivery-limit" .-> dead["cola jobs.dead"]

    classDef q fill:#efe,stroke:#8a8
    class convert,retry,dead q
```

Decisión de ack/nack (la toma `consumer.go` a partir del error del `Processor`):

- **ack** — éxito; o `ErrNotClaimable` (reentrega duplicada / ya terminado / cancelado);
  o `ErrCancelled`; o `ErrExhausted` (ya marcado `dead`); o `ErrPermanent` (MIME/archivo).
- **nack (requeue=false)** — transitorio (Postgres o el store caídos, timeout) → DLX →
  `jobs.retry` → vuelve a `jobs.convert` a los ~30 s.

---

## 3. Secuencia del camino feliz (con base de datos)

```mermaid
sequenceDiagram
    participant Seed as cmd/seed (API)
    participant DB as Postgres
    participant Store as Object store
    participant MQ as RabbitMQ
    participant Worker as cmd/worker
    participant Proc as job.Processor

    Seed->>Store: PUT originals/owner/doc (bytes)
    Seed->>DB: TX INSERT documents + jobs(queued)
    Seed->>MQ: Publish {job_id}
    MQ-->>Worker: delivery
    Worker->>Proc: Process(ctx, job_id)
    Proc->>DB: Claim(job_id, lease)  %% UPDATE condicional
    DB-->>Proc: ClaimedJob (storage_key, mime, filename) / 0 filas -> ErrNotClaimable
    Proc-)DB: heartbeat cada 10s (renueva lease)
    Proc->>Store: GET originals/{storage_key} + LimitReader
    Store-->>Proc: bytes / 404 -> ErrNotFound
    Proc->>Proc: converter.Convert (parse -> segmentar -> render -> validar)
    Proc->>Store: PUT bundles/owner/bundle/NN.md + index.md
    Proc->>DB: Publish = TX (INSERT bundle + UPDATE job=succeeded)
    Proc->>Proc: notifier.JobChanged (best-effort, hoy log)
    Proc-->>Worker: nil (ok)
    Worker->>MQ: ack
```

Dos órdenes que no se invierten: **primero el store (PUT), después el COMMIT** (al revés
dejaría filas apuntando a archivos inexistentes); y se **toma el trabajo (Claim) antes
de tocar nada**.

---

## 4. Ciclo de vida del trabajo (estados en `jobs.status`)

```mermaid
stateDiagram-v2
    [*] --> queued: seed/API inserta
    queued --> processing: Claim (UPDATE condicional)
    processing --> succeeded: Publish (TX)
    processing --> failed: fallo permanente (MarkFailed)
    processing --> dead: attempts > max (MarkDead)
    failed --> processing: Claim lo re-acepta (reintento)
    processing --> processing: lease vencido -> otro worker re-toma
    succeeded --> [*]
    dead --> [*]
```

- `queued → processing`: solo si `status IN ('queued','failed')` **o** el lease venció.
  Esa condición viaja DENTRO del `WHERE` (comprobarla en Go sería una carrera).
- **Idempotencia**: una reentrega de un job ya `succeeded`/`processing` encuentra 0
  filas → `ErrNotClaimable` → ack, sin reprocesar.
- **Recuperación**: un worker muerto deja el job en `processing`; al vencer su lease,
  otro lo re-toma (sin esto, escalar hacia abajo perdería trabajos).

---

## 5. Arquitectura por piezas (hexagonal)

La regla de dependencia apunta hacia dentro: los adaptadores conocen el núcleo y los
puertos, nunca al revés.

```mermaid
flowchart TB
    subgraph ADAPTERS_IN["Adaptadores primarios"]
        consumer["amqp/consumer.go<br/>(delivery -> job_id, decide ack/nack)"]
    end

    subgraph CORE["Núcleo / aplicación"]
        port["port.JobProcessor / JobRepository / Notifier<br/>(contratos)"]
        proc["job.Processor<br/>(claim -> convertir -> publicar)"]
        conv["converter (parse HTML/EPUB -> segmentar -> render -> validar) [puro]"]
        dom["domain (ClaimedJob, Bundle, ...)"]
    end

    subgraph ADAPTERS_OUT["Adaptadores secundarios"]
        repo["db.Repo (pgx)<br/>Claim/Heartbeat/Publish"]
        store["objectstore.New(cfg)<br/>fs | http"]
        notif["apiclient.LogNotifier<br/>(best-effort)"]
    end

    consumer --> port
    consumer --> proc
    proc --> conv
    proc --> repo
    proc --> store
    proc --> notif
    conv --> dom
    repo --> dom

    note["Composition root: cmd/worker/main.go<br/>enchufa repo + store + notifier + Processor"]
    note -.-> proc

    classDef pure fill:#ffd,stroke:#aa6
    class conv,dom pure
```

---

## Qué es real y qué es andamio (hoy)

| Pieza | Estado |
|---|---|
| Mensajería RabbitMQ (topología, ack/nack, retry/DLX, reconexión, apagado) | ✅ real |
| Base de datos: Claim (idempotencia+lease), Heartbeat, Publish transaccional | ✅ real |
| Servicio `object-storage` (HTTP, Bearer, atómico, path-traversal) | ✅ real |
| Backends del worker: `fs` (carpeta) y `http` (servicio por red) | ✅ real |
| Conversor real: segmentador + renderer (frontmatter/index/log) + validador (§6) | ✅ real |
| Parsers: HTML y EPUB (`text/html`, `application/epub+zip`) | ✅ real |
| Notificación a la API (`apiclient.LogNotifier`) | 🟡 registra en log (el POST real llega con la API) |
| Otros parsers (markdown, texto, docx, pdf) y API real | ⛔ pendientes (ver `PENDIENTES.md`) |
| Docker / compose | ⛔ **paso 4 (pendiente)** |
