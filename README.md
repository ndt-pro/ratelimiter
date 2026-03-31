# ratelimiter

A flexible rate limiting library for Go. Supports in-memory and Redis storage backends, with first-class middleware for Gin and gorilla/mux (or any `net/http`-compatible router).

## Features

- **Fluent API** — `PerMinute`, `PerHour`, `PerDay`, `None`, fluent `.By(key)` and `.Response(fn)` builders
- **Named limiters** — Register reusable rules by name, resolved per-request via a `KeyFunc`
- **Multiple limits per route** — Combine per-minute + per-hour limits; each tracked independently
- **Pluggable storage** — In-memory (default) or Redis; implement the `Store` interface for anything else
- **Gin & Mux middleware** — Drop-in middleware with standard `X-RateLimit-*` headers
- **Custom responses** — Override the 429 response per limit rule
- **Thread-safe** — Safe for concurrent use; in-memory store uses a mutex, Redis store uses atomic Lua scripts

## Installation

```bash
go get github.com/ndt-pro/ratelimiter
```

For Gin middleware:
```bash
go get github.com/ndt-pro/ratelimiter/gin
```

For Mux / net/http middleware:
```bash
go get github.com/ndt-pro/ratelimiter/mux
```

## Quick Start

### With Gin

```go
package main

import (
    "net/http"

    "github.com/gin-gonic/gin"
    ratelimiter "github.com/ndt-pro/ratelimiter"
    ginrl "github.com/ndt-pro/ratelimiter/gin"
)

func main() {
    // Create a limiter with the default in-memory store.
    limiter := ratelimiter.New()

    // Register a named limiter.
    limiter.For("api", func(r *http.Request) []ratelimiter.Limit {
        return []ratelimiter.Limit{
            ratelimiter.PerMinute(60).By(r.RemoteAddr),
        }
    })

    r := gin.Default()
    r.Use(ginrl.RateLimit(limiter, "api"))

    r.GET("/ping", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"message": "pong"})
    })

    r.Run(":8080")
}
```

### With gorilla/mux

```go
package main

import (
    "fmt"
    "net/http"

    "github.com/gorilla/mux"
    ratelimiter "github.com/ndt-pro/ratelimiter"
    muxrl "github.com/ndt-pro/ratelimiter/mux"
)

func main() {
    limiter := ratelimiter.New()

    limiter.For("api", func(r *http.Request) []ratelimiter.Limit {
        return []ratelimiter.Limit{
            ratelimiter.PerMinute(60).By(r.RemoteAddr),
        }
    })

    router := mux.NewRouter()
    router.Use(muxrl.RateLimit(limiter, "api"))

    router.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintln(w, "pong")
    })

    http.ListenAndServe(":8080", router)
}
```

## Storage Backends

### In-Memory (default)

The default store keeps counters in memory. It is suitable for single-process applications and local development.

```go
// Default GC interval (1 minute)
limiter := ratelimiter.New()

// Custom GC interval
limiter := ratelimiter.New(
    ratelimiter.WithMemoryGCInterval(5 * time.Minute),
)
```

> If you use the in-memory store, call `store.Stop()` when shutting down to release the GC goroutine. Access the store via `ratelimiter.NewMemoryStore` directly if you need to stop it.

### Redis

Use Redis for distributed applications where multiple instances share rate limit counters.

```go
import (
    "github.com/redis/go-redis/v9"
    ratelimiter "github.com/ndt-pro/ratelimiter"
)

rdb := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

limiter := ratelimiter.New(
    ratelimiter.WithRedis(rdb),
)
```

### Custom Store

Implement the `Store` interface to use any backend (e.g. Memcached, DynamoDB):

```go
type Store interface {
    Hit(ctx context.Context, key string, ttl time.Duration) (int64, error)
    Get(ctx context.Context, key string) (int64, error)
    Reset(ctx context.Context, key string) error
    AvailableIn(ctx context.Context, key string) (time.Duration, error)
}

limiter := ratelimiter.New(
    ratelimiter.WithStore(myCustomStore),
)
```

## Limit Builder

| Function | Description |
|---|---|
| `PerMinute(n)` | Allow `n` requests per minute |
| `PerMinutes(m, n)` | Allow `n` requests per `m` minutes |
| `PerHour(n)` | Allow `n` requests per hour |
| `PerHours(m, n)` | Allow `n` requests per `m` hours |
| `PerDay(n)` | Allow `n` requests per day |
| `PerDays(m, n)` | Allow `n` requests per `m` days |
| `None()` | Unlimited — bypasses rate limiting |

Chain methods:

```go
// Differentiate counters by key (IP, user ID, etc.)
ratelimiter.PerMinute(60).By(r.RemoteAddr)

// Key by authenticated user with IP fallback
ratelimiter.PerMinute(100).By(userIDOrIP(r))

// Custom response when limit is exceeded
ratelimiter.PerMinute(5).By(r.RemoteAddr).Response(func(w http.ResponseWriter, r *http.Request) {
    http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
})
```

## Named Limiters

Named limiters let you register rules once and apply them by name to routes.

```go
// Multiple limits — the per-minute and per-hour counters are tracked independently.
limiter.For("login", func(r *http.Request) []ratelimiter.Limit {
    return []ratelimiter.Limit{
        ratelimiter.PerMinute(5).By(r.RemoteAddr),
        ratelimiter.PerHour(20).By(r.RemoteAddr),
    }
})

// Conditional limiting — bypass for admin users.
limiter.For("api", func(r *http.Request) []ratelimiter.Limit {
    if isAdmin(r) {
        return []ratelimiter.Limit{ratelimiter.None()}
    }
    return []ratelimiter.Limit{
        ratelimiter.PerMinute(60).By(r.RemoteAddr),
    }
})
```

## HTTP Response Headers

The middlewares automatically set standard rate limit headers:

| Header | Description |
|---|---|
| `X-RateLimit-Limit` | Maximum requests allowed in the window |
| `X-RateLimit-Remaining` | Requests remaining in the current window |
| `Retry-After` | Seconds to wait before retrying (only on 429) |

## Low-Level API

You can use the limiter directly without middleware:

```go
ctx := context.Background()

// Increment and check in one call.
result, err := limiter.Attempt(ctx, "login", ratelimiter.PerMinute(5).By(ip))
if !result.Allowed {
    fmt.Printf("Rate limited. Retry after %s\n", result.RetryAfter)
}

// Check without incrementing.
tooMany, err := limiter.TooManyAttempts(ctx, "login:"+ip, 5)

// Get remaining attempts.
remaining, err := limiter.Remaining(ctx, "login:"+ip, 5)

// Get time until reset.
d, err := limiter.AvailableIn(ctx, "login:"+ip)

// Reset the counter.
err = limiter.Clear(ctx, "login:"+ip)
```

## Key Prefix

Avoid key collisions when sharing a Redis instance across multiple services:

```go
limiter := ratelimiter.New(
    ratelimiter.WithRedis(rdb),
    ratelimiter.WithKeyPrefix("myapp"),
)
```

## Running Tests

```bash
go test ./...
```

Redis store tests use [miniredis](https://github.com/alicebob/miniredis) — no real Redis instance required.

## License

MIT
