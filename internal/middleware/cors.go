package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSConfig holds CORS configuration
type corsConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	ExposedHeaders []string
	MaxAge         string
}

// CORSConfig returns default CORS configuration
func CORSConfig(evolutionBaseURL string) *corsConfig {
	allowedOrigins := []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
	}

	// Add Evolution base URL if not empty
	if evolutionBaseURL != "" {
		allowedOrigins = append(allowedOrigins, evolutionBaseURL)
	}

	// Add frontend URLs if defined
	if frontendURL := os.Getenv("FRONTEND_URL"); frontendURL != "" {
		allowedOrigins = append(allowedOrigins, frontendURL)
	}
	if evocloudURL := os.Getenv("EVOCLOUD_FRONTEND_URL"); evocloudURL != "" {
		allowedOrigins = append(allowedOrigins, evocloudURL)
	}

	return &corsConfig{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{
			"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS",
		},
		AllowedHeaders: []string{
			"Origin", "Content-Type", "Content-Length", "Accept-Encoding",
			"X-CSRF-Token", "Authorization", "api_access_token", "X-Requested-With",
			// The browser sends the active company on every Core call. Without
			// this name in the preflight answer, the save never leaves the page
			// (API keys, agents, folders).
			"X-Evo-Tenant-Id",
		},
		ExposedHeaders: []string{
			"Content-Length", "Authorization",
		},
		MaxAge: "86400", // Cache preflight for 24 hours
	}
}

// CORS returns a Gin middleware that handles CORS
func CORS(config *corsConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// Set CORS headers for all requests (including preflight)
		if origin != "" {
			// Always allow the requesting origin (for development)
			// In production, you may want to check isAllowedOrigin first
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		} else {
			// No origin header (e.g., same-origin request or Postman)
			c.Header("Access-Control-Allow-Origin", "*")
		}

		// Set CORS policy headers
		c.Header("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
		c.Header("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
		c.Header("Access-Control-Expose-Headers", strings.Join(config.ExposedHeaders, ", "))
		c.Header("Access-Control-Max-Age", config.MaxAge)

		// Handle preflight requests (OPTIONS)
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// isAllowedOrigin checks if the origin is in the allowed origins list
func isAllowedOrigin(origin string, allowedOrigins []string) bool {
	for _, allowedOrigin := range allowedOrigins {
		if origin == allowedOrigin {
			return true
		}
	}
	return false
}
