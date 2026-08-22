package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"okfbundler/internal/adapters/events"
	"okfbundler/internal/adapters/http/middleware"
	"okfbundler/internal/app"
	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

// jobListItem es una fila de GET /jobs. Sus claves json coinciden con lo que el
// frontend espera para la tabla (id, filename, size, createdAt).
type jobListItem struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

type JobsHandler struct {
	Jobs      ports.JobRepository
	Hub       *events.Hub
	DeleteJob *app.DeleteJobService
}

func NewJobsHandler(jobs ports.JobRepository, hub *events.Hub, del *app.DeleteJobService) *JobsHandler {
	return &JobsHandler{Jobs: jobs, Hub: hub, DeleteJob: del}
}

// Delete borra un job propio junto con su documento original y su bundle
// (si existe), en la base de datos y en el object store.
func (h *JobsHandler) Delete(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserIDKey)

	job, err := h.Jobs.FindByID(c.Request.Context(), c.Param("id"))
	if err != nil || job.OwnerID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "trabajo no encontrado"})
		return
	}

	if err := h.DeleteJob.Delete(c.Request.Context(), job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo eliminar el trabajo"})
		return
	}

	c.Status(http.StatusNoContent)
}

// List devuelve los trabajos del usuario autenticado (fuente de la lista del
// dashboard; sustituye al historial en localStorage, que era por-navegador).
func (h *JobsHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserIDKey)

	jobs, err := h.Jobs.ListByOwner(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo listar los trabajos"})
		return
	}

	out := make([]jobListItem, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, jobListItem{ID: j.ID, Filename: j.Filename, Size: j.SizeBytes, CreatedAt: j.CreatedAt})
	}
	c.JSON(http.StatusOK, out)
}

func (h *JobsHandler) Get(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserIDKey)

	job, err := h.Jobs.FindByID(c.Request.Context(), c.Param("id"))
	// 404 tanto si no existe como si es de otro usuario: no revelamos que el
	// recurso existe (sección 6, "aislamiento").
	if err != nil || job.OwnerID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "trabajo no encontrado"})
		return
	}

	// job.MarshalJSON traduce el estado del worker al del frontend.
	c.JSON(http.StatusOK, job)
}

// Stream expone el cambio de estado del job por Server-Sent Events, para que el
// frontend no tenga que hacer polling. Un evento de texto por línea:
// "event: status\ndata: {...}\n\n", que EventSource del navegador entiende.
//
// La fuente del evento es el trigger pg_notify del worker (ver
// migrations/0002 y db.ListenJobStatus): el worker escribe el estado en
// Postgres, el trigger avisa, el hub lo reparte. El status que llega es el del
// worker (queued/succeeded/...); se traduce al del frontend antes de emitirlo.
func (h *JobsHandler) Stream(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserIDKey)
	jobID := c.Param("id")

	// Nos suscribimos ANTES de leer el estado actual: así, si el job cambia
	// justo entre el Subscribe y el FindByID, igual llega el evento en vez de
	// perderse en la ventana de la carrera.
	ch := h.Hub.Subscribe(jobID)
	defer h.Hub.Unsubscribe(jobID, ch)

	job, err := h.Jobs.FindByID(c.Request.Context(), jobID)
	if err != nil || job.OwnerID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "trabajo no encontrado"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	if !writeStatusEvent(c, job.Status.Frontend()) || job.Status.Terminal() {
		return
	}

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			status := domain.JobStatus(evt.Status)
			if !writeStatusEvent(c, status.Frontend()) || status.Terminal() {
				return
			}
		}
	}
}

func writeStatusEvent(c *gin.Context, status string) bool {
	if _, err := fmt.Fprintf(c.Writer, "event: status\ndata: {\"status\":%q}\n\n", status); err != nil {
		return false
	}
	c.Writer.Flush()
	return true
}
