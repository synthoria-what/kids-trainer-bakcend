package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kids-trainer/backend/internal/auth"
)

func Auth(service *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") { c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"}); return }
		claims, err := service.Parse(strings.TrimPrefix(header, "Bearer "))
		if err != nil { c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"}); return }
		c.Set("user_id", claims.UserID); c.Set("role", claims.Role); c.Next()
	}
}
func RequireRole(role string) gin.HandlerFunc { return func(c *gin.Context) { value, _ := c.Get("role"); if value != role { c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"}); return }; c.Next() } }
func UserID(c *gin.Context) int { value, _ := c.Get("user_id"); id, _ := value.(int); return id }
