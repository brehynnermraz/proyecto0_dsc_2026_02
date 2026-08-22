const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function parseError(res: Response): Promise<never> {
  let message = res.statusText;
  try {
    const body = await res.json();
    if (body?.error) message = body.error;
  } catch {
    // el body no era JSON, se usa el statusText
  }
  throw new ApiError(res.status, message);
}

export async function register(email: string, password: string): Promise<{ id: string }> {
  const res = await fetch(`${API_URL}/auth/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) await parseError(res);
  return res.json();
}

export async function login(email: string, password: string): Promise<{ token: string }> {
  const res = await fetch(`${API_URL}/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) await parseError(res);
  return res.json();
}

// Solo los formatos que el worker (../worker) soporta hoy: HTML y EPUB.
// Markdown/texto/docx/pdf están pendientes allá, así que no se ofrecen.
export type DocumentFormat = "html" | "epub";

export class UploadAbortedError extends Error {}

// Usa XMLHttpRequest en vez de fetch porque fetch no expone progreso de
// subida (solo de descarga) — la barra de progreso real del dropzone
// depende de xhr.upload.onprogress.
export function uploadDocument(
  token: string,
  file: File,
  format: DocumentFormat,
  onProgress?: (fraction: number) => void
): { promise: Promise<{ job_id: string; status: string }>; abort: () => void } {
  const xhr = new XMLHttpRequest();
  const form = new FormData();
  form.append("file", file);
  form.append("format", format);

  const promise = new Promise<{ job_id: string; status: string }>((resolve, reject) => {
    xhr.open("POST", `${API_URL}/documents`);
    xhr.setRequestHeader("Authorization", `Bearer ${token}`);

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress?.(e.loaded / e.total);
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(JSON.parse(xhr.responseText));
        return;
      }
      let message = xhr.statusText;
      try {
        const body = JSON.parse(xhr.responseText);
        if (body?.error) message = body.error;
      } catch {
        // el body no era JSON, se usa el statusText
      }
      reject(new ApiError(xhr.status, message));
    };

    xhr.onerror = () => reject(new ApiError(0, "no se pudo conectar con el servidor"));
    xhr.onabort = () => reject(new UploadAbortedError("subida cancelada"));

    xhr.send(form);
  });

  return { promise, abort: () => xhr.abort() };
}

export type JobStatus = "pending" | "processing" | "done" | "failed";

// Los campos coinciden con lo que emite domain.Job.MarshalJSON en el backend.
// Status ya viene traducido al vocabulario del frontend (el backend mapea el
// enum del worker queued/succeeded/... a pending/done/...). BundleID llega
// cuando el worker ya publicó el bundle (habilita la descarga).
export interface Job {
  ID: string;
  DocumentID: string;
  OwnerID: string;
  Status: JobStatus;
  Attempts: number;
  Error: string;
  BundleID: string | null;
  CreatedAt: string;
}

// JobListItem es una fila de GET /jobs: lo que el dashboard necesita para la
// tabla. El estado en vivo y el bundle los trae luego cada fila con getJob/SSE.
export interface JobListItem {
  id: string;
  filename: string;
  size: number;
  createdAt: string;
}

// listJobs es la FUENTE de la lista de trabajos (sustituye al historial en
// localStorage, que era por-navegador y no se veía en incógnito ni en otro
// equipo). El backend filtra por el dueño del token.
export async function listJobs(token: string): Promise<JobListItem[]> {
  const res = await fetch(`${API_URL}/jobs`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) await parseError(res);
  return res.json();
}

export async function getJob(token: string, jobId: string): Promise<Job> {
  const res = await fetch(`${API_URL}/jobs/${jobId}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) await parseError(res);
  return res.json();
}

export async function deleteJob(token: string, jobId: string): Promise<void> {
  const res = await fetch(`${API_URL}/jobs/${jobId}`, {
    method: "DELETE",
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) await parseError(res);
}

// El endpoint /jobs/:id/events es SSE pero requiere Authorization: Bearer, y
// EventSource nativo del navegador no permite headers custom. Por eso se lee
// el stream a mano con fetch + ReadableStream en vez de usar EventSource.
export function streamJobStatus(
  token: string,
  jobId: string,
  onStatus: (status: JobStatus) => void,
  onError?: (err: unknown) => void
): () => void {
  const controller = new AbortController();

  (async () => {
    try {
      const res = await fetch(`${API_URL}/jobs/${jobId}/events`, {
        headers: { Authorization: `Bearer ${token}` },
        signal: controller.signal,
      });
      if (!res.ok || !res.body) {
        await parseError(res);
        return;
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        let sep;
        while ((sep = buffer.indexOf("\n\n")) !== -1) {
          const rawEvent = buffer.slice(0, sep);
          buffer = buffer.slice(sep + 2);

          const dataLine = rawEvent.split("\n").find((l) => l.startsWith("data:"));
          if (!dataLine) continue;
          try {
            const payload = JSON.parse(dataLine.slice(5).trim());
            if (payload.status) onStatus(payload.status as JobStatus);
          } catch {
            // línea data mal formada, se ignora
          }
        }
      }
    } catch (err) {
      if (controller.signal.aborted) return;
      onError?.(err);
    }
  })();

  return () => controller.abort();
}

export async function downloadBundle(token: string, bundleId: string): Promise<Blob> {
  const res = await fetch(`${API_URL}/bundles/${bundleId}/download`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!res.ok) await parseError(res);
  return res.blob();
}
