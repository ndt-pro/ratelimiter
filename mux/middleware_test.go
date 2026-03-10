package mux_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	ratelimiter "github.com/ndt-pro/ratelimiter"
	muxrl "github.com/ndt-pro/ratelimiter/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMuxLimiter() *ratelimiter.Limiter {
	return ratelimiter.New(
		ratelimiter.WithStore(ratelimiter.NewMemoryStore(time.Minute)),
	)
}

func setupMuxRouter(limiter *ratelimiter.Limiter, name string) http.Handler {
	r := mux.NewRouter()
	r.Use(muxrl.RateLimit(limiter, name))
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}).Methods(http.MethodGet)
	return r
}

func TestMuxMiddleware_AllowsRequests(t *testing.T) {
	l := newMuxLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(5).By(r.RemoteAddr)}
	})

	router := setupMuxRouter(l, "api")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:0"
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "4", w.Header().Get("X-RateLimit-Remaining"))
}

func TestMuxMiddleware_BlocksWhenLimitExceeded(t *testing.T) {
	l := newMuxLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(2).By(r.RemoteAddr)}
	})

	router := setupMuxRouter(l, "api")

	makeReq := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "5.6.7.8:0"
		router.ServeHTTP(w, req)
		return w
	}

	w1 := makeReq()
	assert.Equal(t, http.StatusOK, w1.Code)
	w2 := makeReq()
	assert.Equal(t, http.StatusOK, w2.Code)

	w3 := makeReq()
	assert.Equal(t, http.StatusTooManyRequests, w3.Code)
	assert.Equal(t, "0", w3.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w3.Header().Get("Retry-After"))
}

func TestMuxMiddleware_RetryAfterHeader(t *testing.T) {
	l := newMuxLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(1).By(r.RemoteAddr)}
	})

	router := setupMuxRouter(l, "api")

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

func TestMuxMiddleware_UnknownLimiter_FailsOpen(t *testing.T) {
	l := newMuxLimiter()
	router := setupMuxRouter(l, "unknown")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMuxMiddleware_DifferentIPsTrackedSeparately(t *testing.T) {
	l := newMuxLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(1).By(r.RemoteAddr)}
	})

	router := setupMuxRouter(l, "api")

	sendReq := func(ip string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip + ":0"
		router.ServeHTTP(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusOK, sendReq("100.0.0.1"))
	assert.Equal(t, http.StatusOK, sendReq("100.0.0.2"))

	assert.Equal(t, http.StatusTooManyRequests, sendReq("100.0.0.1"))
	assert.Equal(t, http.StatusTooManyRequests, sendReq("100.0.0.2"))
}

func TestMuxMiddleware_CustomResponseFunc(t *testing.T) {
	l := newMuxLimiter()
	l.For("api", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{
			ratelimiter.PerMinute(1).By(r.RemoteAddr).Response(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("custom"))
			}),
		}
	})

	router := setupMuxRouter(l, "api")

	makeReq := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "200.0.0.1:0"
		router.ServeHTTP(w, req)
		return w
	}

	makeReq()
	w := makeReq()
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "custom", w.Body.String())
}

func TestMuxMiddleware_None_AlwaysAllowed(t *testing.T) {
	l := newMuxLimiter()
	l.For("unlimited", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.None()}
	})

	router := setupMuxRouter(l, "unlimited")

	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}
}

func TestMuxMiddleware_WithStandardHTTPHandler(t *testing.T) {
	l := newMuxLimiter()
	l.For("plain", func(r *http.Request) []ratelimiter.Limit {
		return []ratelimiter.Limit{ratelimiter.PerMinute(3).By(r.RemoteAddr)}
	})

	handler := muxrl.RateLimit(l, "plain")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "50.0.0.1:0"
		handler.ServeHTTP(w, req)
		return w
	}

	assert.Equal(t, http.StatusOK, makeReq().Code)
	assert.Equal(t, http.StatusOK, makeReq().Code)
	assert.Equal(t, http.StatusOK, makeReq().Code)
	assert.Equal(t, http.StatusTooManyRequests, makeReq().Code)
}
