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

// Start performs the first cache refresh synchronously, then launches a
// background goroutine that repeats every ttl until ctx is cancelled.
// The synchronous first fill ensures the HTTP server never serves a stale
// empty cache on startup and that readiness probes reflect real API reachability.
func (m *Manager) Start(ctx context.Context) {
	m.refresh(ctx) // synchronous first fill
	go func() {
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

// filterByKind returns the subset of targets whose route_kind label matches kind.
func filterByKind(targets []SDTarget, kind string) []SDTarget {
	var out []SDTarget
	for _, t := range targets {
		if t.Labels["route_kind"] == kind {
			out = append(out, t)
		}
	}
	return out
}

// Handler returns an http.HandlerFunc that serves the cached targets as a JSON
// array conforming to the Prometheus HTTP SD format.
func (m *Manager) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		cache := m.cache
		m.mu.RUnlock()

		result := cache
		if kind := r.URL.Query().Get("kind"); kind != "" {
			result = filterByKind(cache, kind)
		}

		if result == nil {
			result = []SDTarget{}
		}

		data, err := json.Marshal(result)
		if err != nil {
			http.Error(w, "failed to marshal targets", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
