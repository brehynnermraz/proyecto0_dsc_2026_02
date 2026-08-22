package handlers

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"okfbundler/internal/adapters/http/middleware"
	"okfbundler/internal/ports"
)

type BundlesHandler struct {
	Bundles ports.BundleRepository
	Storage ports.ObjectStore
}

func NewBundlesHandler(bundles ports.BundleRepository, storage ports.ObjectStore) *BundlesHandler {
	return &BundlesHandler{Bundles: bundles, Storage: storage}
}

func (h *BundlesHandler) Download(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserIDKey)

	bundle, err := h.Bundles.FindByID(c.Request.Context(), c.Param("id"))
	if err != nil || bundle.OwnerID != userID {
		c.JSON(http.StatusNotFound, gin.H{"error": "bundle no encontrado"})
		return
	}

	stream, err := h.Storage.GetBundleStream(c.Request.Context(), bundle.StorageKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo leer el bundle"})
		return
	}
	defer stream.Close()

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="bundle.zip"`)
	c.Status(http.StatusOK)
	io.Copy(c.Writer, stream)
}
