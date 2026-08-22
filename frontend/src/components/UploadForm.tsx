"use client";

import { DragEvent, useRef, useState } from "react";
import { uploadDocument, ApiError, UploadAbortedError, DocumentFormat } from "@/lib/api";
import { RecentJob } from "@/lib/job-history";
import { formatBytes } from "@/lib/format";
import { useAuth } from "@/context/AuthContext";

const FORMATS: { value: DocumentFormat; label: string; extensions: string }[] = [
  { value: "markdown", label: "Markdown", extensions: ".md,.markdown" },
  { value: "html", label: "HTML", extensions: ".html,.htm" },
  { value: "text", label: "texto", extensions: ".txt" },
];

const MAX_SIZE_BYTES = 20 * 1024 * 1024;

type UploadState = "staged" | "uploading" | "done" | "error";

export default function UploadForm({ onUploaded }: { onUploaded?: (job: RecentJob) => void }) {
  const { token } = useAuth();
  const inputRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<(() => void) | null>(null);

  const [format, setFormat] = useState<DocumentFormat>("markdown");
  const [dragActive, setDragActive] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [state, setState] = useState<UploadState>("staged");
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [doneJobId, setDoneJobId] = useState<string | null>(null);

  function reset() {
    setFile(null);
    setState("staged");
    setProgress(0);
    setError(null);
    setDoneJobId(null);
    abortRef.current = null;
  }

  // Deja el archivo en espera: la subida real solo arranca cuando el
  // usuario confirma con el botón "Subir archivo".
  function stageFile(selected: File) {
    setFile(selected);
    setProgress(0);
    if (selected.size > MAX_SIZE_BYTES) {
      setState("error");
      setError(`El archivo supera el máximo de ${formatBytes(MAX_SIZE_BYTES)}`);
      return;
    }
    setState("staged");
    setError(null);
  }

  function startUpload() {
    if (!token || !file) return;

    setState("uploading");
    setProgress(0);
    setError(null);

    const { promise, abort } = uploadDocument(token, file, format, setProgress);
    abortRef.current = abort;

    promise
      .then(({ job_id }) => {
        setState("done");
        setDoneJobId(job_id);
        onUploaded?.({ id: job_id, filename: file.name, size: file.size, createdAt: new Date().toISOString() });
      })
      .catch((err) => {
        if (err instanceof UploadAbortedError) {
          setState("staged");
          setProgress(0);
          return;
        }
        setState("error");
        setError(err instanceof ApiError ? err.message : "No se pudo subir el documento");
      });
  }

  function handleDrop(e: DragEvent<HTMLDivElement>) {
    e.preventDefault();
    setDragActive(false);
    const dropped = e.dataTransfer.files?.[0];
    if (dropped) stageFile(dropped);
  }

  return (
    <div>
      {!file && (
        <div className="rounded-lg border border-neutral-200 bg-white p-8">
          <h2 className="text-2xl font-semibold text-neutral-900">Cargar documento</h2>
          <p className="mt-1.5 text-sm text-neutral-500">Elige el formato y arrastra el archivo</p>

          <div className="mt-6 flex justify-between gap-3">
            {FORMATS.map((f) => (
              <button
                key={f.value}
                type="button"
                onClick={() => setFormat(f.value)}
                className={`flex-1 rounded-md border px-4 py-2 text-sm font-medium transition ${
                  format === f.value
                    ? "border-neutral-900 bg-neutral-900 text-white"
                    : "border-neutral-300 bg-white text-neutral-700 hover:border-neutral-400"
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>

          <div
            onDragOver={(e) => {
              e.preventDefault();
              setDragActive(true);
            }}
            onDragLeave={() => setDragActive(false)}
            onDrop={handleDrop}
            onClick={() => inputRef.current?.click()}
            className={`mt-6 flex min-h-[22rem] cursor-pointer flex-col items-center justify-center gap-4 rounded-md px-6 py-16 text-center transition ${
              dragActive ? "bg-neutral-200" : "bg-neutral-100 hover:bg-neutral-200/70"
            }`}
          >
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="text-neutral-900">
              <path d="M12 16V4M12 4l-5 5M12 4l5 5" strokeLinecap="round" strokeLinejoin="round" />
              <path d="M4 16v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <div>
              <p className="text-base text-neutral-600">
                Arrastra tu archivo aquí o <span className="font-medium text-neutral-900">explora tus archivos</span>
              </p>
              <p className="mt-2 text-sm text-neutral-400">Tamaño máximo 20MB</p>
            </div>
            <input
              ref={inputRef}
              type="file"
              accept={FORMATS.find((f) => f.value === format)?.extensions}
              className="hidden"
              onChange={(e) => {
                const selected = e.target.files?.[0];
                e.target.value = "";
                if (selected) stageFile(selected);
              }}
            />
          </div>
        </div>
      )}

      {file && state === "done" && (
        <div className="rounded-lg border border-neutral-200 bg-white p-6">
          <div className="flex items-center gap-3">
            <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-green-500 text-white">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3">
                <path d="M5 13l4 4L19 7" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </div>
            <h2 className="text-xl font-semibold text-neutral-900">Trabajo creado</h2>
          </div>

          <div className="mt-4 flex items-center gap-3 rounded-md bg-neutral-100 px-4 py-3">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="shrink-0 text-neutral-900">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" strokeLinejoin="round" />
              <path d="M14 2v6h6" strokeLinejoin="round" />
            </svg>
            <span className="min-w-0 flex-1 truncate text-sm text-neutral-900">{file.name}</span>
            <span className="shrink-0 text-sm text-neutral-500">{formatBytes(file.size)}</span>
          </div>

          <p className="mt-4 text-center text-xs text-neutral-400">
            id del trabajo: <span className="text-neutral-600">{doneJobId}</span>
          </p>
        </div>
      )}

      {file && state !== "done" && (
        <div className="rounded-lg border border-neutral-200 bg-white p-4">
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-neutral-200 text-neutral-900">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" strokeLinejoin="round" />
                <path d="M14 2v6h6" strokeLinejoin="round" />
              </svg>
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium text-neutral-900">{file.name}</p>
              <p className="text-xs text-neutral-400">{formatBytes(file.size)}</p>
            </div>
            <button
              type="button"
              onClick={() => {
                if (state === "uploading") abortRef.current?.();
                reset();
              }}
              aria-label="Quitar archivo"
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-red-50 text-red-500 hover:bg-red-100"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                <path d="M6 6l12 12M18 6L6 18" strokeLinecap="round" />
              </svg>
            </button>
          </div>

          {state === "staged" && (
            <button
              type="button"
              onClick={startUpload}
              className="mt-3 w-full rounded-md bg-neutral-900 py-2.5 text-sm font-semibold text-white transition hover:bg-black"
            >
              Subir archivo
            </button>
          )}

          {state === "uploading" && (
            <div className="mt-3">
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-neutral-100">
                <div
                  className="h-full rounded-full bg-neutral-900 transition-all"
                  style={{ width: `${Math.round(progress * 100)}%` }}
                />
              </div>
              <p className="mt-1.5 text-xs text-neutral-400">{Math.round(progress * 100)}% — subiendo...</p>
            </div>
          )}

          {state === "error" && (
            <>
              {error && <p className="mt-3 text-xs font-medium text-red-600">{error}</p>}
              {file && file.size <= MAX_SIZE_BYTES && (
                <button
                  type="button"
                  onClick={startUpload}
                  className="mt-3 w-full rounded-md bg-neutral-900 py-2.5 text-sm font-semibold text-white transition hover:bg-black"
                >
                  Reintentar
                </button>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
