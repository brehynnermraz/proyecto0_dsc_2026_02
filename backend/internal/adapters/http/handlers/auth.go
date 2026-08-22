package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"okfbundler/internal/domain"
	"okfbundler/internal/ports"
)

type AuthHandler struct {
	Users  ports.UserRepository
	Tokens ports.TokenIssuer
}

func NewAuthHandler(users ports.UserRepository, tokens ports.TokenIssuer) *AuthHandler {
	return &AuthHandler{Users: users, Tokens: tokens}
}

type credentials struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var body credentials
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo procesar la contraseña"})
		return
	}

	user := &domain.User{ID: uuid.NewString(), Email: body.Email, PasswordHash: string(hash)}
	if err := h.Users.Create(c.Request.Context(), user); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "el usuario ya existe"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": user.ID})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var body credentials
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.Users.FindByEmail(c.Request.Context(), body.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "credenciales inválidas"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "credenciales inválidas"})
		return
	}

	token, err := h.Tokens.Issue(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo emitir el token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token})
}
