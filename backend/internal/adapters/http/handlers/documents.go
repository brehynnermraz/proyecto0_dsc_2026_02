package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"okfbundler/internal/adapters/http/middleware"
	"okfbundler/internal/app"
)

// maxUploadBytes acota el tamaño aceptado en la carga; ajustar según lo que
// el equipo decida soportar (sección 11: tratar la entrada como no confiable).
const maxUploadBytes = 20 << 20 // 20 MiB

type DocumentsHandler struct {
	Submit *app.SubmitDocumentService
}

func NewDocumentsHandler(submit *app.SubmitDocumentService) *DocumentsHandler {
	return &DocumentsHandler{Submit: submit}
}

func (h *DocumentsHandler) Upload(c *gin.Context) {
	ownerID := c.GetString(middleware.ContextUserIDKey)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "falta el archivo 'file'"})
		return
	}
	defer file.Close()

	if header.Size > maxUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "el archivo excede el tamaño máximo"})
		return
	}

	format := c.PostForm("format") // "markdown" | "html" | "text"

	jobID, err := h.Submit.Submit(c.Request.Context(), ownerID, header.Filename, format, header.Size, header.Header.Get("Content-Type"), file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo encolar el trabajo"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID, "status": "pending"})
}
