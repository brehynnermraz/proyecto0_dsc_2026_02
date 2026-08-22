package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"okfbundler/internal/adapters/events"
	"okfbundler/internal/adapters/http/middleware"
	"okfbundler/internal/app"
	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

type JobsHandler struct {
	Jobs      ports.JobRepository
	Hub       *events.Hub
	DeleteJob *app.DeleteJobService
}

func NewJobsHandler(jobs ports.JobRepository, hub *events.Hub, del *app.DeleteJobService) *JobsHandler {
	return &JobsHandler{Jobs: jobs, Hub: hub, DeleteJob: del}
}

// jobStatusWebhookRequest es el payload que el Worker envía al terminar de
// procesar un job. status determina qué campos son obligatorios: "done"
// requiere bundle_id, "failed" requiere error.
type jobStatusWebhookRequest struct {
	Status   string `json:"status" binding:"required,oneof=done failed"`
	BundleID string `json:"bundle_id"`
	Attempt  int    `json:"attempt"`
	Error    string `json:"error"`
}

// Webhook recibe la notificación del Worker cuando termina (con éxito o con
// error) de procesar un job. Autenticado por secreto compartido, no por JWT
// de usuario (ver middleware.RequireWebhookSecret). Al escribir el nuevo
// estado en la DB, el trigger jobs_notify_status dispara pg_notify, que la
// API ya escucha (ver db.ListenJobStatus) para reenviarlo por SSE — así que
// aquí no hace falta publicar en el Hub a mano.
func (h *JobsHandler) Webhook(c *gin.Context) {
	jobID := c.Param("id")

	job, err := h.Jobs.FindByID(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "trabajo no encontrado"})
		return
	}

	var req jobStatusWebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload inválido"})
		return
	}

	switch req.Status {
	case string(domain.JobDone):
		if req.BundleID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bundle_id es requerido cuando status=done"})
			return
		}
		if err := h.Jobs.MarkDone(c.Request.Context(), job.ID, req.BundleID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo actualizar el trabajo"})
			return
		}
	case string(domain.JobFailed):
		if req.Error == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "error es requerido cuando status=failed"})
			return
		}
		if err := h.Jobs.MarkFailed(c.Request.Context(), job.ID, req.Attempt, req.Error); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo actualizar el trabajo"})
			return
		}
	}

	c.Status(http.StatusNoContent)
}

// Delete borra un job propio junto con su documento original y su bundle
// (si existe), en la base de datos y en el almacenamiento de objetos.
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

func (h *JobsHandler) Get(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserIDKey)

	job, err := h.Jobs.FindByID(c.Request.Context(), c.Param("id"))
	// 404 tanto si no existe como si es de otro usuario: no revelamos que el
	// recurso existe (sección 6, "aislamiento").
	if err != nil || job.OwnerID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "trabajo no encontrado"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// Stream expone el cambio de estado del job por Server-Sent Events, para que
// el frontend no tenga que hacer polling a Get. Un evento de texto por
// línea: "event: status\ndata: {...}\n\n", que EventSource del navegador
// entiende nativamente.
func (h *JobsHandler) Stream(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserIDKey)
	jobID := c.Param("id")

	// Nos suscribimos ANTES de leer el estado actual: así, si el job cambia
	// justo entre el Subscribe y el FindByID, igual llega el evento en vez
	// de perderse en la ventana de la carrera.
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

	if !writeStatusEvent(c, string(job.Status)) || isTerminal(job.Status) {
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
			if !writeStatusEvent(c, evt.Status) || isTerminal(domain.JobStatus(evt.Status)) {
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

func isTerminal(s domain.JobStatus) bool {
	return s == domain.JobDone || s == domain.JobFailed
}
