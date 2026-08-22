# Frontend — OKF Bundler

Next.js 14 (App Router + TypeScript + Tailwind). Consume la API de
`../backend` por HTTP desde el navegador: sube documentos, sigue el estado
de conversión en vivo y descarga el bundle resultante.

## Cómo arrancar

**Con Docker Compose**, ver el `docker-compose.yml` en la raíz del repo.

**Localmente**, requiere Node 18.17+ (Next.js 14 no arranca con Node 16):

```
npm install
npm run dev
```

Abre [http://localhost:3000](http://localhost:3000). Necesita el backend
corriendo (ver `../backend/README.md`) y `FRONTEND_ORIGIN=http://localhost:3000`
configurado ahí para que CORS deje pasar las peticiones del navegador.

## Variables de entorno

| Variable | Default | Uso |
|---|---|---|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` | URL base de la API a la que apunta todo `src/lib/api.ts` |

Se define en `.env.local` (no versionado; ver `.env.example`). Es
`NEXT_PUBLIC_*` porque el cliente (navegador) la necesita en tiempo de
ejecución, no solo el servidor de Next.

## Estructura

```
src/
  app/
    layout.tsx              layout raíz: fuente (Roboto Mono) + AuthProvider
    (auth)/                  grupo de rutas sin nav, pantalla completa
      layout.tsx               shell de dos columnas (formulario + ilustración)
      login/page.tsx
      register/page.tsx
    (app)/                   grupo de rutas con el nav de la app
      layout.tsx               nav + contenedor
      page.tsx                  única pantalla autenticada: ver más abajo
  components/
    UploadForm.tsx            tarjeta de carga: dropzone, formato, progreso, confirmación
    JobTableRow.tsx             una fila de la tabla de trabajos, con estado en vivo y menú de acciones
    AuthIllustration.tsx        ilustración SVG del login/registro
    Nav.tsx                     logo + logout
    PasswordInput.tsx           input de contraseña con mostrar/ocultar
  lib/
    api.ts                     cliente HTTP de la API (fetch/XHR), sin dependencias externas
    useJobStatus.ts             hook: trae el estado de un job y se suscribe por SSE mientras no sea terminal
    job-status.ts                labels/colores de estado, uses compartidos
    job-history.ts               historial local de trabajos en localStorage (ver nota abajo)
    format.ts                    formateo de bytes
  context/
    AuthContext.tsx             JWT en localStorage, expuesto vía useAuth()
```

## Flujo de la pantalla principal (`/`)

No hay rutas separadas para "cargar" y "ver trabajos": todo vive en
`app/(app)/page.tsx`, con tres vistas condicionadas por estado local:

1. **Sin trabajos** — pantalla vacía con un botón para empezar.
2. **Con trabajos** — tabla (`id`, nombre, tamaño, estado, acción) con un
   botón "Cargar archivo" arriba a la derecha.
3. **Cargando** — al hacer click en "Cargar archivo" desde cualquiera de las
   dos anteriores, aparece la tarjeta de subida (`UploadForm`) con un link
   para volver.

Cada fila de la tabla (`JobTableRow`) consulta su propio estado al montar y
se suscribe al stream SSE de ese job mientras no llegue a un estado
terminal — no hay un poll centralizado ni un websocket único: cada fila es
independiente.

## Por qué no hay una API para "listar mis trabajos"

El backend no expone `GET /jobs` (por diseño — no estaba en el alcance). El
frontend compensa guardando un historial liviano en `localStorage`
(`lib/job-history.ts`, clave `okf_recent_jobs`) con lo mínimo para poder
volver a preguntarle a la API por cada uno: `id`, `filename`, `size`,
`createdAt`. **No es la fuente de verdad del estado** — el estado real
(`pending`/`processing`/`done`/`failed`) siempre se pide en vivo a
`GET /jobs/:id` y se sigue por SSE; el historial local solo recuerda *cuáles*
`job_id` existen. Esto también significa que el historial es por
navegador/dispositivo, no por cuenta: si el usuario entra desde otro
navegador, no va a ver los trabajos que subió antes (aunque el backend sí
los tenga).

## Notas de diseño

- Fuente global: Roboto Mono (`next/font/google`, ver `app/layout.tsx`).
- Acento de color: negro/`neutral-900` en toda la app.
- El logo (`public/logo.png`, también usado como favicon vía
  `src/app/icon.png`) y el resto de la identidad visual salieron de
  referencias que dio el usuario durante el desarrollo, no de un sistema de
  diseño externo.
- El webhook que usa el worker para notificar que un job terminó
  (`POST /webhooks/jobs/:id/status`) no lo llama el frontend — es
  servidor-a-servidor; ver `../backend/README.md`.
