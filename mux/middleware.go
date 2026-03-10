// Package mux provides a rate limiting middleware compatible with gorilla/mux
// and any net/http-based router.
package mux

import (
	"fmt"
	"net/http"

	ratelimiter "github.com/ndt-pro/ratelimiter"
)

// RateLimit returns a standard net/http middleware that enforces the named
// rate limiter. It is compatible with gorilla/mux, chi, and any router that
// supports func(http.Handler) http.Handler middleware.
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
//	r := mux.NewRouter()
//	r.Use(muxrl.RateLimit(limiter, "api"))
func RateLimit(l *ratelimiter.Limiter, name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, err := l.CheckRequest(r.Context(), name, r)
			if err != nil {
				// Named limiter not registered or store error – fail open.
				next.ServeHTTP(w, r)
				return
			}

			setHeaders(w, result)

			if !result.Allowed {
				if result.Limit.ResponseFunc != nil {
					result.Limit.ResponseFunc(w, r)
					return
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", result.RetryAfter.Seconds()))
				http.Error(w, `{"message":"Too Many Requests"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// setHeaders writes the standard X-RateLimit-* headers.
func setHeaders(w http.ResponseWriter, result ratelimiter.AttemptResult) {
	if result.Limit.MaxAttempts <= 0 {
		return
	}
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit.MaxAttempts))
	if !result.Allowed {
		w.Header().Set("X-RateLimit-Remaining", "0")
	} else {
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
	}
}
