// Antes esto guardaba un historial de trabajos en localStorage, pero era
// por-navegador: no se veía en incógnito ni en otro equipo. Ahora la fuente de
// la lista es el servidor (GET /jobs, ver lib/api.ts listJobs). Este archivo
// conserva solo el tipo que comparten el dashboard, la tabla y el formulario.
export interface RecentJob {
  id: string;
  filename: string;
  size: number;
  createdAt: string;
}
