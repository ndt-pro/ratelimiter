package gin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	ratelimiter "github.com/ndt-pro/ratelimiter"
	ginrl "github.com/ndt-pro/ratelimiter/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newGinLimiter() *ratelimiter.Limiter {
	return ratelimiter.New(
		ratelimiter.WithStore(ratelimiter.NewMemoryStore(time.Minute)),
	)
}

func setupGinRouter(limiter *ratelimiter.Limiter, name string) *gin.Engine {
	r := gin.New()
	r.Use(ginrl.RateLimit(limiter, name))
	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func TestGinMiddleware_AllowsRequests(t *testing.T) {
	l := newGinLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(5).By(r.RemoteAddr)}
	})

	router := setupGinRouter(l, "api")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:0"
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "4", w.Header().Get("X-RateLimit-Remaining"))
}

func TestGinMiddleware_BlocksWhenLimitExceeded(t *testing.T) {
	l := newGinLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(2).By(r.RemoteAddr)}
	})

	router := setupGinRouter(l, "api")

	makeReq := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "5.6.7.8:0"
		router.ServeHTTP(w, req)
		return w
	}

	// First two allowed.
	w1 := makeReq()
	assert.Equal(t, http.StatusOK, w1.Code)
	w2 := makeReq()
	assert.Equal(t, http.StatusOK, w2.Code)

	// Third should be rate limited.
	w3 := makeReq()
	assert.Equal(t, http.StatusTooManyRequests, w3.Code)
	assert.Equal(t, "0", w3.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w3.Header().Get("Retry-After"))
}

func TestGinMiddleware_RetryAfterHeader(t *testing.T) {
	l := newGinLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(1).By(r.RemoteAddr)}
	})

	router := setupGinRouter(l, "api")

	makeReq := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "9.10.11.12:0"
		router.ServeHTTP(w, req)
		return w
	}

	makeReq()      // allowed
	w := makeReq() // denied

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	retryAfter := w.Header().Get("Retry-After")
	require.NotEmpty(t, retryAfter)
}

func TestGinMiddleware_UnknownLimiter_FailsOpen(t *testing.T) {
	l := newGinLimiter()
	// Intentionally don't register "unknown".
	router := setupGinRouter(l, "unknown")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(w, req)

	// Fail open: should return 200.
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGinMiddleware_DifferentIPsTrackedSeparately(t *testing.T) {
	l := newGinLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(1).By(r.RemoteAddr)}
	})

	router := setupGinRouter(l, "api")

	sendReq := func(ip string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip + ":0"
		router.ServeHTTP(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusOK, sendReq("100.0.0.1"))
	assert.Equal(t, http.StatusOK, sendReq("100.0.0.2")) // different IP, still allowed

	assert.Equal(t, http.StatusTooManyRequests, sendReq("100.0.0.1")) // same IP, blocked
	assert.Equal(t, http.StatusTooManyRequests, sendReq("100.0.0.2")) // same IP, blocked
}

func TestGinMiddleware_CustomResponseFunc(t *testing.T) {
	l := newGinLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{
			ratelimiter.PerMinute(1).By(r.RemoteAddr).Response(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("custom"))
			}),
		}
	})

	router := setupGinRouter(l, "api")

	makeReq := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "200.0.0.1:0"
		router.ServeHTTP(w, req)
		return w
	}

	makeReq()      // allowed
	w := makeReq() // triggers custom response
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "custom", w.Body.String())
}

func TestGinMiddleware_None_AlwaysAllowed(t *testing.T) {
	l := newGinLimiter()
	l.For("unlimited", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.None()}
	})

	router := setupGinRouter(l, "unlimited")

	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}
