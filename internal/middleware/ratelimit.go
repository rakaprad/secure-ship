package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter is a per-client token bucket limiter.
//
// Clients are identified by the TCP peer address. X-Forwarded-For is
// intentionally ignored: it is client-controlled, so trusting it without a
// known proxy in front would let anyone bypass the limit by spoofing it.
type RateLimiter struct {
	rps   rate.Limit
	burst int
	ttl   time.Duration
	now   func() time.Time

	mu        sync.Mutex
	clients   map[string]*client
	lastSweep time.Time
}

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter allows each client rps requests per second with the given
// burst. Clients idle for longer than ttl are forgotten, which bounds memory.
func NewRateLimiter(rps float64, burst int, ttl time.Duration) *RateLimiter {
	return &RateLimiter{
		rps:     rate.Limit(rps),
		burst:   burst,
		ttl:     ttl,
		now:     time.Now,
		clients: make(map[string]*client),
	}
}

// Allow reports whether a request from key may proceed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	rl.sweep(now)

	c, ok := rl.clients[key]
	if !ok {
		c = &client{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.clients[key] = c
	}
	c.lastSeen = now
	return c.limiter.AllowN(now, 1)
}

// sweep drops idle clients at most once per ttl. Doing this inline avoids a
// background goroutine that would need its own lifecycle management.
func (rl *RateLimiter) sweep(now time.Time) {
	if now.Sub(rl.lastSweep) < rl.ttl {
		return
	}
	for k, c := range rl.clients {
		if now.Sub(c.lastSeen) > rl.ttl {
			delete(rl.clients, k)
		}
	}
	rl.lastSweep = now
}

// Middleware rejects requests over the limit with 429 Too Many Requests.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}` + "\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
