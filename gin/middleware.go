// Package gin provides a rate limiting middleware for the Gin web framework.
package gin

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	ratelimiter "github.com/ndt-pro/ratelimiter"
)

// RateLimit returns a Gin middleware that enforces the named rate limiter.
// The limiter must be registered on the Limiter instance via limiter.For(name, ...).
//
// When the rate limit is exceeded, the middleware writes the appropriate
// X-RateLimit-* headers and terminates the request with HTTP 429.
// If the Limit has a custom ResponseFunc, it is called instead.
//
// Usage:
//
//	limiter := ratelimiter.New()
//	limiter.For("api", func(r *http.Request) []ratelimiter.Limit {
//	    return []ratelimiter.Limit{ratelimiter.PerMinute(60).By(r.RemoteAddr)}
//	})
//
//	r := gin.Default()
//	r.Use(ginrl.RateLimit(limiter, "api"))
func RateLimit(l *ratelimiter.Limiter, name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, err := l.CheckRequest(c.Request.Context(), name, c.Request)
		if err != nil {
			// Named limiter not registered or store error – fail open and log.
			_ = c.Error(err)
			c.Next()
			return
		}

		setHeaders(c, result)

		if !result.Allowed {
			if result.Limit.ResponseFunc != nil {
				result.Limit.ResponseFunc(c.Writer, c.Request)
				c.Abort()
				return
			}
			c.Header("Retry-After", fmt.Sprintf("%.0f", result.RetryAfter.Seconds()))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"message": "Too Many Requests",
			})
			return
		}

		c.Next()
	}
}

// setHeaders writes the standard X-RateLimit-* headers.
func setHeaders(c *gin.Context, result ratelimiter.AttemptResult) {
	if result.Limit.MaxAttempts <= 0 {
		return
	}
	c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit.MaxAttempts))
	c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
	if !result.Allowed {
		c.Header("X-RateLimit-Remaining", "0")
	}
}
