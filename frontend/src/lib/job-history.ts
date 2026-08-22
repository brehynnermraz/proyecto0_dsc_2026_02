// El API no expone un endpoint para listar los jobs de un usuario, así que
// el dashboard guarda un historial liviano en localStorage solo como
// conveniencia de navegación (no es la fuente de verdad del estado).
const STORAGE_KEY = "okf_recent_jobs";
const MAX_ENTRIES = 20;

export interface RecentJob {
  id: string;
  filename: string;
  size: number;
  createdAt: string;
}

export function getRecentJobs(): RecentJob[] {
  if (typeof window === "undefined") return [];
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "[]");
  } catch {
    return [];
  }
}

export function addRecentJob(entry: RecentJob) {
  const existing = getRecentJobs().filter((j) => j.id !== entry.id);
  const next = [entry, ...existing].slice(0, MAX_ENTRIES);
  localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
}

export function removeRecentJob(id: string) {
  const next = getRecentJobs().filter((j) => j.id !== id);
  localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
}
