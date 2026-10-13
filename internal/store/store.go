// Package store defines the persistence boundary for short links.
//
// Handlers depend only on the Store interface, so the in-memory
// implementation can be swapped for Redis or Postgres without touching
// the HTTP layer.
package store

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when no link exists for a code.
	ErrNotFound = errors.New("store: link not found")
	// ErrConflict is returned when a code is already taken.
	ErrConflict = errors.New("store: code already exists")
	// ErrFull is returned when the store has reached its capacity.
	ErrFull = errors.New("store: capacity reached")
)

// Link is a shortened URL and its usage counter.
type Link struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	Hits      int64     `json:"hits"`
}

// Store persists links.
type Store interface {
	// Create saves a new link. It returns ErrConflict if the code is taken
	// and ErrFull if the store cannot accept more links.
	Create(ctx context.Context, l Link) error
	// Get returns the link for code, or ErrNotFound.
	Get(ctx context.Context, code string) (Link, error)
	// Resolve returns the link for code and increments its hit counter
	// atomically, or returns ErrNotFound.
	Resolve(ctx context.Context, code string) (Link, error)
}
