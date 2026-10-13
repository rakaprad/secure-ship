package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestMemoryCreateGet(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(0)

	if err := m.Create(ctx, Link{Code: "abc", URL: "https://example.com"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Create(ctx, Link{Code: "abc", URL: "https://other.example"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create: got %v, want ErrConflict", err)
	}
	l, err := m.Get(ctx, "abc")
	if err != nil || l.URL != "https://example.com" {
		t.Fatalf("Get: got %+v, %v", l, err)
	}
	if _, err := m.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: got %v, want ErrNotFound", err)
	}
}

func TestMemoryCapacity(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(1)
	if err := m.Create(ctx, Link{Code: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Create(ctx, Link{Code: "b"}); !errors.Is(err, ErrFull) {
		t.Fatalf("got %v, want ErrFull", err)
	}
}

func TestMemoryResolveConcurrent(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(0)
	if err := m.Create(ctx, Link{Code: "abc"}); err != nil {
		t.Fatal(err)
	}

	const n = 100
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Resolve(ctx, "abc"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	l, _ := m.Get(ctx, "abc")
	if l.Hits != n {
		t.Fatalf("hits = %d, want %d", l.Hits, n)
	}
}
