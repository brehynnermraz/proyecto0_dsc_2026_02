package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS permite que el frontend (servido desde otro origen, ej. Next.js en
// localhost:3000) llame a la API desde el navegador. Solo se refleja el
// origen configurado explícitamente, nunca "*", porque las rutas protegidas
// usan Authorization: Bearer y credenciales no deben habilitarse para
// cualquier origen.
func CORS(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if allowedOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowedOrigin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
