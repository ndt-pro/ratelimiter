package ratelimiter

import (
	"net/http"
	"time"
)

// Limit holds the configuration for a rate limit rule.
type Limit struct {
	// MaxAttempts is the maximum number of attempts allowed within DecayPeriod.
	MaxAttempts int

	// DecayPeriod is the time window for the rate limit.
	DecayPeriod time.Duration

	// Key is an optional segment to differentiate rate limits (e.g. by IP or user ID).
	// When set, it is appended to the limiter name to form the final store key.
	Key string

	// ResponseFunc allows customizing the HTTP response when the rate limit is exceeded.
	// If nil, a default 429 Too Many Requests response is sent.
	ResponseFunc func(w http.ResponseWriter, r *http.Request)
}

// By sets the key segment for this limit and returns the updated Limit.
// The key is typically derived from the request (e.g. IP address, user ID).
//
//	ratelimiter.PerMinute(60).By(r.RemoteAddr)
func (l Limit) By(key string) Limit {
	l.Key = key
	return l
}

// Response sets a custom response function that is called when this limit is exceeded.
//
//	ratelimiter.PerMinute(5).By(ip).Response(func(w http.ResponseWriter, r *http.Request) {
//	    http.Error(w, "slow down", http.StatusTooManyRequests)
//	})
func (l Limit) Response(fn func(w http.ResponseWriter, r *http.Request)) Limit {
	l.ResponseFunc = fn
	return l
}

// isUnlimited reports whether this limit has no restriction (created via None()).
func (l Limit) isUnlimited() bool {
	return l.MaxAttempts <= 0
}

// PerMinute returns a Limit that allows max attempts per minute.
func PerMinute(max int) Limit {
	return Limit{
		MaxAttempts: max,
		DecayPeriod: time.Minute,
	}
}

// PerMinutes returns a Limit that allows max attempts per n minutes.
func PerMinutes(n int, max int) Limit {
	return Limit{
		MaxAttempts: max,
		DecayPeriod: time.Duration(n) * time.Minute,
	}
}

// PerHour returns a Limit that allows max attempts per hour.
func PerHour(max int) Limit {
	return Limit{
		MaxAttempts: max,
		DecayPeriod: time.Hour,
	}
}

// PerHours returns a Limit that allows max attempts per n hours.
func PerHours(n int, max int) Limit {
	return Limit{
		MaxAttempts: max,
		DecayPeriod: time.Duration(n) * time.Hour,
	}
}

// PerDay returns a Limit that allows max attempts per day.
func PerDay(max int) Limit {
	return Limit{
		MaxAttempts: max,
		DecayPeriod: 24 * time.Hour,
	}
}

// PerDays returns a Limit that allows max attempts per n days.
func PerDays(n int, max int) Limit {
	return Limit{
		MaxAttempts: max,
		DecayPeriod: time.Duration(n) * 24 * time.Hour,
	}
}

// None returns a Limit with no restriction (unlimited).
// Useful for bypassing rate limiting for certain users/conditions.
func None() Limit {
	return Limit{MaxAttempts: -1}
}
