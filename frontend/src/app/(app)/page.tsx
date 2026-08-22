"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/context/AuthContext";
import UploadForm from "@/components/UploadForm";
import JobTableRow from "@/components/JobTableRow";
import { getRecentJobs, addRecentJob, removeRecentJob, RecentJob } from "@/lib/job-history";

export default function DashboardPage() {
  const { token, ready } = useAuth();
  const router = useRouter();
  const [recentJobs, setRecentJobs] = useState<RecentJob[]>([]);
  const [showForm, setShowForm] = useState(false);

  useEffect(() => {
    if (ready && !token) router.replace("/login");
  }, [ready, token, router]);

  useEffect(() => {
    setRecentJobs(getRecentJobs());
  }, []);

  if (!ready || !token) return null;

  function handleUploaded(job: RecentJob) {
    addRecentJob(job);
    setRecentJobs((prev) => [job, ...prev.filter((j) => j.id !== job.id)].slice(0, 20));
  }

  function handleDeleted(id: string) {
    removeRecentJob(id);
    setRecentJobs((prev) => prev.filter((j) => j.id !== id));
  }

  if (showForm) {
    return (
      <div className="mx-auto w-full max-w-2xl">
        <button
          type="button"
          onClick={() => setShowForm(false)}
          className="mb-6 inline-flex items-center gap-2 text-sm text-neutral-600 hover:text-neutral-900"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M19 12H5M12 19l-7-7 7-7" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          Volver a mis archivos subidos
        </button>

        <UploadForm onUploaded={handleUploaded} />
      </div>
    );
  }

  if (recentJobs.length === 0) {
    return (
      <div className="mx-auto flex w-full max-w-md flex-col items-center text-center">
        <h1 className="text-3xl font-semibold text-neutral-900">No hay archivos cargados</h1>
        <p className="mt-3 text-sm text-neutral-500">Haz click en el botón para comenzar</p>
        <button
          type="button"
          onClick={() => setShowForm(true)}
          className="mt-6 rounded-md bg-neutral-900 px-6 py-3 text-sm font-medium text-white transition hover:bg-black"
        >
          Cargar archivo
        </button>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-5xl">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-neutral-900">Trabajos creados</h1>
        <button
          type="button"
          onClick={() => setShowForm(true)}
          className="rounded-md bg-neutral-900 px-4 py-2 text-sm font-medium text-white transition hover:bg-black"
        >
          Cargar archivo
        </button>
      </div>

      <div className="overflow-x-auto rounded-lg border border-neutral-200 bg-white">
        <table className="w-full">
          <thead>
            <tr className="border-b border-neutral-200 text-left text-xs font-semibold uppercase tracking-wide text-neutral-500">
              <th className="px-4 py-3">id</th>
              <th className="px-4 py-3">nombre del archivo</th>
              <th className="px-4 py-3">tamaño</th>
              <th className="px-4 py-3">estado</th>
              <th className="px-4 py-3 text-right">acción</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-neutral-100">
            {recentJobs.map((job) => (
              <JobTableRow key={job.id} job={job} onDeleted={handleDeleted} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
