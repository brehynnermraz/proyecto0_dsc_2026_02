import { JobStatus } from "./api";

export const STATUS_LABEL: Record<JobStatus, string> = {
  pending: "En espera",
  processing: "Procesando",
  done: "Completado",
  failed: "Falló",
};

export const STATUS_DOT_COLOR: Record<JobStatus, string> = {
  pending: "bg-neutral-400",
  processing: "bg-amber-500",
  done: "bg-green-500",
  failed: "bg-red-500",
};

export function isTerminal(status: JobStatus): boolean {
  return status === "done" || status === "failed";
}
