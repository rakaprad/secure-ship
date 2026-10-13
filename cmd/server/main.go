// Command server runs the secure-ship URL shortener.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rakaprad/secure-ship/internal/api"
	"github.com/rakaprad/secure-ship/internal/middleware"
	"github.com/rakaprad/secure-ship/internal/store"
)

type config struct {
	addr          string
	baseURL       *url.URL
	rateRPS       float64
	rateBurst     int
	maxLinks      int
	shutdownDelay time.Duration
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ready := &atomic.Bool{}
	srv := api.New(store.NewMemory(cfg.maxLinks), cfg.baseURL, log, ready)
	limiter := middleware.NewRateLimiter(cfg.rateRPS, cfg.rateBurst, 10*time.Minute)

	// Probes are registered outside the rate limiter so kubelet checks are
	// never throttled.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.Healthz)
	mux.HandleFunc("GET /readyz", srv.Readyz)
	mux.Handle("/", limiter.Middleware(srv.Routes()))

	httpServer := &http.Server{
		Addr:              cfg.addr,
		Handler:           middleware.Chain(mux, middleware.Recover(log), middleware.Logger(log), middleware.SecurityHeaders),
		ReadHeaderTimeout: 5 * time.Second, // mitigates Slowloris
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.addr, "base_url", cfg.baseURL.String())
		errCh <- httpServer.ListenAndServe()
	}()
	ready.Store(true)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	// Fail readiness first and wait, so Kubernetes removes the pod from
	// Service endpoints before we stop accepting connections.
	log.Info("shutdown signal received", "drain_delay", cfg.shutdownDelay.String())
	ready.Store(false)
	time.Sleep(cfg.shutdownDelay)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("server stopped")
	return nil
}

func loadConfig() (config, error) {
	cfg := config{
		addr:          env("ADDR", ":8080"),
		rateRPS:       5,
		rateBurst:     10,
		maxLinks:      100_000,
		shutdownDelay: 5 * time.Second,
	}

	base, err := url.Parse(env("BASE_URL", "http://localhost:8080"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return cfg, fmt.Errorf("BASE_URL must be an absolute http(s) URL")
	}
	cfg.baseURL = base

	if v, ok := os.LookupEnv("RATE_LIMIT_RPS"); ok {
		if cfg.rateRPS, err = strconv.ParseFloat(v, 64); err != nil || cfg.rateRPS <= 0 {
			return cfg, fmt.Errorf("RATE_LIMIT_RPS must be a positive number")
		}
	}
	if v, ok := os.LookupEnv("RATE_LIMIT_BURST"); ok {
		if cfg.rateBurst, err = strconv.Atoi(v); err != nil || cfg.rateBurst <= 0 {
			return cfg, fmt.Errorf("RATE_LIMIT_BURST must be a positive integer")
		}
	}
	if v, ok := os.LookupEnv("MAX_LINKS"); ok {
		if cfg.maxLinks, err = strconv.Atoi(v); err != nil || cfg.maxLinks <= 0 {
			return cfg, fmt.Errorf("MAX_LINKS must be a positive integer")
		}
	}
	if v, ok := os.LookupEnv("SHUTDOWN_DELAY"); ok {
		if cfg.shutdownDelay, err = time.ParseDuration(v); err != nil || cfg.shutdownDelay < 0 {
			return cfg, fmt.Errorf("SHUTDOWN_DELAY must be a non-negative duration such as 5s")
		}
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
