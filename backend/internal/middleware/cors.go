package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func CORS(origins []string) gin.HandlerFunc {
	allowed := map[string]bool{}; for _, origin := range origins { allowed[strings.TrimSpace(origin)] = true }
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if allowed[origin] { c.Header("Access-Control-Allow-Origin", origin); c.Header("Vary", "Origin") }
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if c.Request.Method == http.MethodOptions { c.AbortWithStatus(http.StatusNoContent); return }; c.Next()
	}
}
