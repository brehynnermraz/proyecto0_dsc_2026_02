package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"okfbundler/internal/ports"
)

const ContextUserIDKey = "user_id"

func RequireAuth(tokens ports.TokenIssuer) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if token == "" || token == header {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "falta el token"})
			return
		}

		userID, err := tokens.Verify(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token inválido"})
			return
		}

		c.Set(ContextUserIDKey, userID)
		c.Next()
	}
}
