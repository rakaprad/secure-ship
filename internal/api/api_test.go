package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rakaprad/secure-ship/internal/store"
)

func newTestServer(t *testing.T, maxLinks int) (*Server, http.Handler) {
	t.Helper()
	base, _ := url.Parse("http://sho.rt")
	ready := &atomic.Bool{}
	ready.Store(true)
	s := New(store.NewMemory(maxLinks), base, slog.New(slog.NewTextHandler(io.Discard, nil)), ready)
	return s, s.Routes()
}

func create(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateRedirectStats(t *testing.T) {
	_, h := newTestServer(t, 0)

	rec := create(t, h, `{"url":"https://example.com/docs?q=1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status %d, body %s", rec.Code, rec.Body)
	}
	var created createResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if !validCode(created.Code) {
		t.Fatalf("generated code %q is not valid", created.Code)
	}
	if want := "http://sho.rt/" + created.Code; created.ShortURL != want || rec.Header().Get("Location") != want {
		t.Fatalf("short_url = %q, Location = %q, want %q", created.ShortURL, rec.Header().Get("Location"), want)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+created.Code, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("redirect: status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example.com/docs?q=1" {
		t.Fatalf("redirect Location = %q", loc)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/links/"+created.Code, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: status %d", rec.Code)
	}
	var link store.Link
	if err := json.NewDecoder(rec.Body).Decode(&link); err != nil {
		t.Fatal(err)
	}
	if link.Hits != 1 {
		t.Fatalf("hits = %d, want 1", link.Hits)
	}
}

func TestCreateValidation(t *testing.T) {
	_, h := newTestServer(t, 0)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"empty body", ``, http.StatusBadRequest},
		{"not json", `url=https://example.com`, http.StatusBadRequest},
		{"unknown field", `{"url":"https://example.com","admin":true}`, http.StatusBadRequest},
		{"missing url", `{}`, http.StatusBadRequest},
		{"relative url", `{"url":"/etc/passwd"}`, http.StatusBadRequest},
		{"javascript scheme", `{"url":"javascript:alert(1)"}`, http.StatusBadRequest},
		{"data scheme", `{"url":"data:text/html,<script>alert(1)</script>"}`, http.StatusBadRequest},
		{"file scheme", `{"url":"file:///etc/passwd"}`, http.StatusBadRequest},
		{"no host", `{"url":"https://"}`, http.StatusBadRequest},
		{"credentials", `{"url":"https://bank.example@evil.example"}`, http.StatusBadRequest},
		{"self link", `{"url":"http://sho.rt/abc1234"}`, http.StatusBadRequest},
		{"too long", `{"url":"https://example.com/` + strings.Repeat("a", maxURLLength) + `"}`, http.StatusBadRequest},
		{"body too large", `{"url":"https://example.com/` + strings.Repeat("a", maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
		{"valid http", `{"url":"http://example.com"}`, http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := create(t, h, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestCreateRequiresJSONContentType(t *testing.T) {
	_, h := newTestServer(t, 0)
	req := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(`{"url":"https://example.com"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestCreateCapacity(t *testing.T) {
	_, h := newTestServer(t, 1)
	if rec := create(t, h, `{"url":"https://example.com"}`); rec.Code != http.StatusCreated {
		t.Fatalf("first create: %d", rec.Code)
	}
	if rec := create(t, h, `{"url":"https://example.com"}`); rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("second create: %d, want 507", rec.Code)
	}
}

func TestNotFound(t *testing.T) {
	_, h := newTestServer(t, 0)
	for _, path := range []string{"/abcdefg", "/api/links/abcdefg", "/bad!code", "/api/links/toolongcode"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, rec.Code)
		}
	}
}

func TestReadyz(t *testing.T) {
	s, _ := newTestServer(t, 0)

	rec := httptest.NewRecorder()
	s.Readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready: status %d", rec.Code)
	}

	s.ready.Store(false)
	rec = httptest.NewRecorder()
	s.Readyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("shutting down: status %d, want 503", rec.Code)
	}
}

func TestNewCodeUnique(t *testing.T) {
	seen := make(map[string]bool)
	for range 10_000 {
		c := newCode()
		if !validCode(c) {
			t.Fatalf("invalid code %q", c)
		}
		if seen[c] {
			t.Fatalf("duplicate code %q", c)
		}
		seen[c] = true
	}
}
