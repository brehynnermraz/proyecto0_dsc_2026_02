// Package apiclient es el adaptador secundario de notificación best-effort del worker
// hacia la API (implementa el puerto port.Notifier).
//
// En producción haría POST :8081/internal/v1/jobs/{id}/changed con X-Internal-Token y
// timeout 2 s, solo para que la API empuje el cambio al navegador por SSE. Es
// best-effort: cuando se llama, la base de datos YA tiene la verdad; NUNCA devuelve
// error ni impide el ack.
//
// PASO 2: como la API todavía no existe, aquí va LogNotifier, que registra el cambio en
// vez de enviarlo. Mantiene el hueco del puerto lleno y visible; el POST real llega
// cuando exista la API.
package apiclient

import (
	"context"
	"log/slog"

	"github.com/uniandes-isis4426/okf/worker/internal/domain"
	"github.com/uniandes-isis4426/okf/worker/internal/port"
)

// LogNotifier implementa port.Notifier registrando el cambio.
type LogNotifier struct {
	log *slog.Logger
}

var _ port.Notifier = (*LogNotifier)(nil)

func NewLogNotifier(log *slog.Logger) *LogNotifier {
	return &LogNotifier{log: log}
}

func (n *LogNotifier) JobChanged(_ context.Context, c domain.JobChange) {
	n.log.Info("notificación a la API (best-effort, simulada)",
		"job_id", c.JobID.String(), "status", c.Status, "bundle_id", c.BundleID, "error", c.Error)
}
