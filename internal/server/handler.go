// Package server provides an HTTP handler that serves Prometheus HTTP SD targets
// from a background-refreshed cache.
package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Ronan-WeScale/k8s_http_discovery/internal/collector"
)

// SDTarget represents a single entry in a Prometheus HTTP SD response.
type SDTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

// Manager holds a set of collectors and maintains a cached slice of SDTargets.
// The cache is refreshed on a configurable TTL by a background goroutine started
// via Start.
type Manager struct {
	collectors []collector.Collector
	cache      []SDTarget
	mu         sync.RWMutex
	ttl        time.Duration
}

// NewManager creates a Manager that will use the given collectors and refresh
// the cache every ttl.
func NewManager(collectors []collector.Collector, ttl time.Duration) *Manager {
	return &Manager{
		collectors: collectors,
		ttl:        ttl,
	}
}

// Start launches a background goroutine that immediately performs the first
// cache refresh and then repeats every ttl until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		m.refresh(ctx)
		ticker := time.NewTicker(m.ttl)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.refresh(ctx)
			}
		}
	}()
}

// refresh runs all collectors and overwrites the cache. Per-collector errors are
// logged and skipped so that a single failing collector does not discard results
// from the others.
func (m *Manager) refresh(ctx context.Context) {
	var targets []SDTarget
	for _, c := range m.collectors {
		ts, err := c.Collect(ctx)
		if err != nil {
			log.Printf("collector %q error: %v", c.Name(), err)
			continue
		}
		for _, t := range ts {
			targets = append(targets, SDTarget{
				Targets: []string{t.URL},
				Labels:  t.Labels,
			})
		}
	}

	m.mu.Lock()
	m.cache = targets
	m.mu.Unlock()
}

// Handler returns an http.HandlerFunc that serves the cached targets as a JSON
// array conforming to the Prometheus HTTP SD format.
func (m *Manager) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		cache := m.cache
		m.mu.RUnlock()

		// Ensure we always return a JSON array (never null).
		if cache == nil {
			cache = []SDTarget{}
		}

		data, err := json.Marshal(cache)
		if err != nil {
			http.Error(w, "failed to marshal targets", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
