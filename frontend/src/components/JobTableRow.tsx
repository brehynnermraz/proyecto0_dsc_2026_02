"use client";

import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/context/AuthContext";
import { downloadBundle, deleteJob, ApiError } from "@/lib/api";
import { useJobStatus } from "@/lib/useJobStatus";
import { STATUS_LABEL } from "@/lib/job-status";
import { formatBytes } from "@/lib/format";
import { RecentJob } from "@/lib/job-history";

const STATUS_TEXT_COLOR: Record<string, string> = {
  pending: "text-neutral-500",
  processing: "text-amber-600",
  done: "text-green-600",
  failed: "text-red-600",
};

export default function JobTableRow({ job, onDeleted }: { job: RecentJob; onDeleted?: (id: string) => void }) {
  const { token } = useAuth();
  const data = useJobStatus(job.id);
  const [menuOpen, setMenuOpen] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function onClickOutside(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false);
    }
    document.addEventListener("mousedown", onClickOutside);
    return () => document.removeEventListener("mousedown", onClickOutside);
  }, []);

  async function handleDownload() {
    if (!token || !data?.BundleID) return;
    setDownloading(true);
    setError(null);
    try {
      const blob = await downloadBundle(token, data.BundleID);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "bundle.zip";
      a.click();
      URL.revokeObjectURL(url);
      setMenuOpen(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo descargar el bundle");
    } finally {
      setDownloading(false);
    }
  }

  async function handleDelete() {
    if (!token) return;
    if (!window.confirm(`¿Eliminar "${job.filename}"? Esta acción no se puede deshacer.`)) return;

    setDeleting(true);
    setError(null);
    try {
      await deleteJob(token, job.id);
      onDeleted?.(job.id);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "No se pudo eliminar el trabajo");
      setDeleting(false);
    }
  }

  const status = data?.Status;

  return (
    <tr className="text-sm">
      <td className="px-4 py-3 font-mono text-xs text-neutral-500" title={job.id}>
        {job.id.slice(0, 8)}…
      </td>
      <td className="max-w-[16rem] truncate px-4 py-3 text-neutral-900" title={job.filename}>
        {job.filename}
      </td>
      <td className="px-4 py-3 text-neutral-500">{job.size ? formatBytes(job.size) : "—"}</td>
      <td className={`px-4 py-3 font-medium uppercase ${status ? STATUS_TEXT_COLOR[status] : "text-neutral-400"}`}>
        {status ? STATUS_LABEL[status] : "…"}
      </td>
      <td className="px-4 py-3 text-right">
        <div ref={menuRef} className="relative inline-block">
          <button
            type="button"
            onClick={() => setMenuOpen((o) => !o)}
            disabled={deleting}
            aria-label="Más opciones"
            className="flex h-7 w-7 items-center justify-center rounded-full text-neutral-400 hover:bg-neutral-100 hover:text-neutral-600 disabled:opacity-50"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
              <circle cx="12" cy="5" r="1.8" />
              <circle cx="12" cy="12" r="1.8" />
              <circle cx="12" cy="19" r="1.8" />
            </svg>
          </button>

          {menuOpen && (
            <div className="absolute right-0 top-full z-10 mt-1 w-48 overflow-hidden rounded-md border border-neutral-200 bg-white py-1 text-left shadow-lg">
              <button
                type="button"
                onClick={handleDownload}
                disabled={status !== "done" || !data?.BundleID || downloading}
                className="w-full px-3 py-2 text-left text-sm text-neutral-700 hover:bg-neutral-50 disabled:cursor-not-allowed disabled:text-neutral-300 disabled:hover:bg-transparent"
              >
                {downloading ? "Descargando..." : "Descargar bundle"}
              </button>
              <button
                type="button"
                onClick={handleDelete}
                disabled={deleting}
                className="w-full px-3 py-2 text-left text-sm text-red-600 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {deleting ? "Eliminando..." : "Eliminar"}
              </button>
              {error && <p className="px-3 py-1.5 text-xs text-red-500">{error}</p>}
            </div>
          )}
        </div>
      </td>
    </tr>
  );
}
