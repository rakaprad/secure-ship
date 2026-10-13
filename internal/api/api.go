// Package api implements the URL shortener's HTTP handlers.
package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rakaprad/secure-ship/internal/store"
)

const (
	alphabet   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLength = 7
	// maxBodyBytes caps request bodies so a client cannot stream an
	// arbitrarily large payload into the JSON decoder.
	maxBodyBytes = 4 << 10
)

// Server holds handler dependencies.
type Server struct {
	store   store.Store
	baseURL *url.URL
	log     *slog.Logger
	ready   *atomic.Bool
}

// New returns a Server. baseURL is the public URL used to build short links
// (for example https://sho.rt). ready is flipped to false during shutdown so
// /readyz stops receiving traffic before the process exits.
func New(s store.Store, baseURL *url.URL, log *slog.Logger, ready *atomic.Bool) *Server {
	return &Server{store: s, baseURL: baseURL, log: log, ready: ready}
}

// Routes registers all endpoints on a new ServeMux.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/links", s.createLink)
	mux.HandleFunc("GET /api/links/{code}", s.getStats)
	mux.HandleFunc("GET /{code}", s.redirect)
	return mux
}

// Healthz reports that the process is alive (liveness probe).
func Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz reports whether the instance should receive traffic (readiness probe).
func (s *Server) Readyz(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "shutting down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type createRequest struct {
	URL string `json:"url"`
}

type createResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
	URL      string `json:"url"`
}

func (s *Server) createLink(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req createRequest
	if err := dec.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "request body must be a JSON object with a \"url\" field")
		return
	}

	target, err := validateTarget(req.URL, s.baseURL.Hostname())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Retry on the (unlikely) event of a code collision: 62^7 ≈ 3.5e12.
	for range 3 {
		link := store.Link{Code: newCode(), URL: target.String(), CreatedAt: time.Now().UTC()}
		err = s.store.Create(r.Context(), link)
		switch {
		case err == nil:
			short := s.baseURL.JoinPath(link.Code).String()
			w.Header().Set("Location", short)
			writeJSON(w, http.StatusCreated, createResponse{Code: link.Code, ShortURL: short, URL: link.URL})
			return
		case errors.Is(err, store.ErrConflict):
			continue
		case errors.Is(err, store.ErrFull):
			writeError(w, http.StatusInsufficientStorage, "link capacity reached")
			return
		default:
			s.log.ErrorContext(r.Context(), "create link", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	s.log.ErrorContext(r.Context(), "create link: code collisions exhausted retries")
	writeError(w, http.StatusInternalServerError, "internal error")
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validCode(code) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	link, err := s.store.Resolve(r.Context(), code)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	// 302 rather than 301: browsers cache 301s forever, which would hide
	// hits from the stats endpoint and make a bad link impossible to fix.
	http.Redirect(w, r, link.URL, http.StatusFound)
}

func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !validCode(code) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	link, err := s.store.Get(r.Context(), code)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

func (s *Server) notFoundOrError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.log.ErrorContext(r.Context(), "store lookup", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// newCode returns a random code using crypto/rand, so codes cannot be
// predicted or enumerated from previously issued ones.
func newCode() string {
	// Bytes >= 248 are rejected so every alphabet character is equally
	// likely (248 = 4 * 62); otherwise the modulo would bias the output.
	const limit = 256 - 256%len(alphabet)
	code := make([]byte, 0, codeLength)
	buf := make([]byte, 16)
	for len(code) < codeLength {
		// crypto/rand.Read never returns an error since Go 1.24.
		_, _ = rand.Read(buf)
		for _, b := range buf {
			if int(b) < limit && len(code) < codeLength {
				code = append(code, alphabet[int(b)%len(alphabet)])
			}
		}
	}
	return string(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
