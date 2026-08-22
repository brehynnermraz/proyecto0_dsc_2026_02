package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireWebhookSecret protege endpoints llamados por servicios internos
// (como el Worker) que no tienen un JWT de usuario. Se autentican con un
// secreto compartido en el header X-Webhook-Secret, comparado en tiempo
// constante para no filtrar el secreto por timing.
func RequireWebhookSecret(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if secret == "" {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "webhook no configurado"})
			return
		}

		got := c.GetHeader("X-Webhook-Secret")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "secreto inválido"})
			return
		}

		c.Next()
	}
}
