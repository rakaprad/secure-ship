package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

func request(h http.Handler, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRateLimitPerClient(t *testing.T) {
	rl := NewRateLimiter(1, 3, time.Minute)
	now := time.Unix(0, 0)
	rl.now = func() time.Time { return now }
	h := rl.Middleware(ok)

	// The burst of 3 is allowed, the 4th request is rejected.
	for i := range 3 {
		if rec := request(h, "10.0.0.1:1234"); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i+1, rec.Code)
		}
	}
	rec := request(h, "10.0.0.1:5678") // different port, same client
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over limit: status %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}

	// Another client has its own bucket.
	if rec := request(h, "10.0.0.2:1234"); rec.Code != http.StatusOK {
		t.Fatalf("other client: status %d, want 200", rec.Code)
	}

	// After one second, one token has been refilled.
	now = now.Add(time.Second)
	if rec := request(h, "10.0.0.1:1234"); rec.Code != http.StatusOK {
		t.Fatalf("after refill: status %d, want 200", rec.Code)
	}
}

func TestRateLimitIgnoresForwardedFor(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute)
	h := rl.Middleware(ok)

	for i, xff := range []string{"1.1.1.1", "2.2.2.2"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		want := http.StatusOK
		if i > 0 {
			want = http.StatusTooManyRequests
		}
		if rec.Code != want {
			t.Fatalf("request %d with spoofed XFF: status %d, want %d", i+1, rec.Code, want)
		}
	}
}

func TestRateLimitSweepsIdleClients(t *testing.T) {
	rl := NewRateLimiter(1, 1, time.Minute)
	now := time.Unix(0, 0)
	rl.now = func() time.Time { return now }

	rl.Allow("a")
	rl.Allow("b")
	now = now.Add(2 * time.Minute)
	rl.Allow("c")

	if n := len(rl.clients); n != 1 {
		t.Fatalf("tracked clients = %d, want 1", n)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := request(SecurityHeaders(ok), "10.0.0.1:1")
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Strict-Transport-Security"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
}

func TestRecover(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	panicky := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	rec := request(Recover(log)(panicky), "10.0.0.1:1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
}
