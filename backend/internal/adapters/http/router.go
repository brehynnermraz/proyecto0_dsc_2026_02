package httpapi

import (
	"github.com/gin-gonic/gin"

	"okfbundler/internal/adapters/events"
	"okfbundler/internal/adapters/http/handlers"
	"okfbundler/internal/adapters/http/middleware"
	"okfbundler/internal/app"
	"okfbundler/internal/ports"
)

type Dependencies struct {
	Submit         *app.SubmitDocumentService
	DeleteJob      *app.DeleteJobService
	Jobs           ports.JobRepository
	Bundles        ports.BundleRepository
	Storage        ports.ObjectStore
	Tokens         ports.TokenIssuer
	Users          ports.UserRepository
	Hub            *events.Hub
	FrontendOrigin string
}

// NewRouter arma la API HTTP: sin estado, no guarda nada de esto en memoria
// entre peticiones más allá de lo que dure el request.
func NewRouter(deps Dependencies) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger(), middleware.CORS(deps.FrontendOrigin))

	r.GET("/healthz", func(c *gin.Context) { c.Status(200) })

	auth := handlers.NewAuthHandler(deps.Users, deps.Tokens)
	r.POST("/auth/register", auth.Register)
	r.POST("/auth/login", auth.Login)

	jobs := handlers.NewJobsHandler(deps.Jobs, deps.Hub, deps.DeleteJob)

	protected := r.Group("/", middleware.RequireAuth(deps.Tokens))
	{
		docs := handlers.NewDocumentsHandler(deps.Submit)
		protected.POST("/documents", docs.Upload)

		protected.GET("/jobs/:id", jobs.Get)
		protected.GET("/jobs/:id/events", jobs.Stream)
		protected.DELETE("/jobs/:id", jobs.Delete)

		bundles := handlers.NewBundlesHandler(deps.Bundles, deps.Storage)
		protected.GET("/bundles/:id/download", bundles.Download)
	}

	// Nota: NO hay endpoint de webhook para el worker. El worker (../worker) es
	// la fuente de verdad y escribe el estado directo en Postgres; el frontend
	// se entera por el camino trigger pg_notify -> LISTEN -> SSE
	// (ver db.ListenJobStatus y jobs.Stream), sin que el worker llame a la API.

	return r
}
