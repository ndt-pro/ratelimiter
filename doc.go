// Package ratelimiter provides a Laravel-style rate limiting library for Go.
//
// # Overview
//
// The package offers named rate limiters with configurable limits (per minute,
// hour, day), pluggable storage (in-memory or Redis), and middleware for Gin
// and gorilla/mux. Use the Limit builder for fluent configuration and Store
// for custom backends.
//
// # Basic Usage
//
// Create a limiter, register a named limiter, and use it in middleware:
//
//	limiter := ratelimiter.New()
//	limiter.For("api", func(r *http.Request) []ratelimiter.Limit {
//	    return []ratelimiter.Limit{
//	        ratelimiter.PerMinute(60).By(r.RemoteAddr),
//	    }
//	})
//
//	r := gin.Default()
//	r.Use(ginrl.RateLimit(limiter, "api"))
//
// # Storage
//
// By default New uses an in-memory store. For Redis:
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	limiter := ratelimiter.New(ratelimiter.WithRedis(rdb))
//
// For a custom backend, implement Store and pass it via WithStore.
//
// # Limit Builder
//
//	PerMinute(n)   - n requests per minute
//	PerHour(n)     - n requests per hour
//	PerDay(n)      - n requests per day
//	None()         - no limit (bypass)
//	limit.By(key)  - segment by IP, user ID, etc.
package ratelimiter
