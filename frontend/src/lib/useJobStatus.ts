import { useEffect, useState } from "react";
import { useAuth } from "@/context/AuthContext";
import { getJob, streamJobStatus, Job } from "@/lib/api";
import { isTerminal } from "@/lib/job-status";

// Consulta el estado de un job al montar y se suscribe por SSE mientras no
// sea terminal, para reflejar en vivo pending -> processing -> done/failed.
export function useJobStatus(jobId: string): Job | null {
  const { token } = useAuth();
  const [data, setData] = useState<Job | null>(null);

  useEffect(() => {
    if (!token) return;
    let cancelled = false;
    let stop: (() => void) | null = null;

    getJob(token, jobId)
      .then((j) => {
        if (cancelled) return;
        setData(j);
        if (isTerminal(j.Status)) return;

        stop = streamJobStatus(
          token,
          jobId,
          (status) => {
            setData((prev) => (prev ? { ...prev, Status: status } : prev));
            if (isTerminal(status)) {
              getJob(token, jobId).then((full) => !cancelled && setData(full));
            }
          },
          () => {}
        );
      })
      .catch(() => {});

    return () => {
      cancelled = true;
      stop?.();
    };
  }, [token, jobId]);

  return data;
}
