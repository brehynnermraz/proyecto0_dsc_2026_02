-- Esquema del paso 2. Es el contrato de datos entre la API (que aquí simula cmd/seed)
-- y el worker: la cola solo entrega trabajo; la FUENTE DE VERDAD del estado es esta
-- base de datos. El worker relee de aquí la storage_key, nunca la deduce.
--
-- Aplicar (Postgres nativo, base okf_learning):
--   psql -h localhost -U "$USER" -d okf_learning -f migrations/0001_init.sql
--
-- Es idempotente (IF NOT EXISTS / DO-blocks): se puede re-ejecutar sin romper nada.
-- Para empezar de cero: DROP DATABASE okf_learning; CREATE DATABASE okf_learning;

CREATE EXTENSION IF NOT EXISTS citext;

-- Los tipos ENUM no admiten "IF NOT EXISTS", así que se guardan en un DO-block.
DO $$ BEGIN
    CREATE TYPE job_status AS ENUM (
        'queued',      -- creado por la API, mensaje publicado
        'processing',  -- un worker lo tomó; heartbeat_at marca que sigue vivo
        'succeeded',   -- bundle publicado
        'failed',      -- fallo reintentable; Claim lo vuelve a aceptar
        'cancelled',   -- lo canceló el usuario
        'dead'         -- agotó max_attempts; terminal
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE validation_status AS ENUM ('valid', 'valid_with_warnings', 'invalid');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS users (
    id            uuid PRIMARY KEY,
    email         citext NOT NULL UNIQUE,
    password_hash text   NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS documents (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename    text NOT NULL,
    mime        text NOT NULL,
    size_bytes  bigint NOT NULL,
    -- Ruta en el object store: originals/{owner_id}/{document_id}. La escribe la API
    -- al cargar y la lee el worker. Nunca se deduce, se lee.
    storage_key text NOT NULL,
    sha256      text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS jobs (
    id            uuid PRIMARY KEY,          -- viaja como MessageId de AMQP
    document_id   uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    owner_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status        job_status NOT NULL DEFAULT 'queued',
    attempts      int  NOT NULL DEFAULT 0,
    max_attempts  int  NOT NULL DEFAULT 3,
    error         text,
    -- Lease: lo renueva el worker que tiene el trabajo (heartbeat). Si deja de
    -- renovarse, otro worker puede re-tomarlo. Sin esta columna, un worker muerto
    -- dejaría el trabajo en 'processing' para siempre y escalar hacia abajo (que ES
    -- matar workers) perdería trabajos en silencio.
    heartbeat_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    started_at    timestamptz,
    finished_at   timestamptz
);

CREATE INDEX IF NOT EXISTS jobs_owner_created_idx ON jobs (owner_id, created_at DESC);

CREATE TABLE IF NOT EXISTS bundles (
    id             uuid PRIMARY KEY,
    -- UNIQUE + ON CONFLICT garantiza "a lo sumo un bundle publicado por trabajo",
    -- aunque dos workers llegaran al final a la vez.
    job_id         uuid NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
    owner_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Prefijo en el object store: bundles/{owner_id}/{bundle_id}
    storage_prefix text NOT NULL,
    validation     validation_status NOT NULL,
    warnings       jsonb NOT NULL DEFAULT '[]'::jsonb,
    concept_count  int    NOT NULL,
    size_bytes     bigint NOT NULL,
    published_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS bundles_owner_idx ON bundles (owner_id);
