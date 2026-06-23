package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ronan-WeScale/k8s_http_discovery/internal/collector"
)

// mockCollector is a test double for collector.Collector.
type mockCollector struct {
	name    string
	targets []collector.Target
	err     error
}

func (m *mockCollector) Name() string { return m.name }
func (m *mockCollector) Collect(_ context.Context) ([]collector.Target, error) {
	return m.targets, m.err
}

func TestHandler_JSON(t *testing.T) {
	t.Parallel()

	mc := &mockCollector{
		name: "mock",
		targets: []collector.Target{
			{URL: "https://foo.example.com/api", Labels: map[string]string{"namespace": "default", "host": "foo.example.com"}},
			{URL: "https://bar.example.com/", Labels: map[string]string{"namespace": "kube-system", "host": "bar.example.com"}},
		},
	}

	mgr := NewManager([]collector.Collector{mc}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	// Give the background goroutine time to populate the cache.
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/targets", nil)
	w := httptest.NewRecorder()
	mgr.Handler()(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var got []SDTarget
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 SDTargets, got %d", len(got))
	}

	for i, want := range mc.targets {
		if len(got[i].Targets) != 1 || got[i].Targets[0] != want.URL {
			t.Errorf("SDTarget[%d].Targets = %v, want [%q]", i, got[i].Targets, want.URL)
		}
		for k, v := range want.Labels {
			if got[i].Labels[k] != v {
				t.Errorf("SDTarget[%d].Labels[%q] = %q, want %q", i, k, got[i].Labels[k], v)
			}
		}
	}
}

func TestHandler_CollectorError_ReturnsPartialResults(t *testing.T) {
	t.Parallel()

	good := &mockCollector{
		name:    "good",
		targets: []collector.Target{{URL: "https://ok.example.com/", Labels: map[string]string{"namespace": "default"}}},
	}
	bad := &mockCollector{
		name: "bad",
		err:  errors.New("simulated collect failure"),
	}

	mgr := NewManager([]collector.Collector{good, bad}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/targets", nil)
	w := httptest.NewRecorder()
	mgr.Handler()(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", resp.StatusCode)
	}

	var got []SDTarget
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Only the good collector's result should be present.
	if len(got) != 1 {
		t.Fatalf("expected 1 SDTarget from good collector, got %d", len(got))
	}
	if got[0].Targets[0] != "https://ok.example.com/" {
		t.Errorf("unexpected target URL: %s", got[0].Targets[0])
	}
}

func TestHandler_EmptyCache_ReturnsEmptyArray(t *testing.T) {
	t.Parallel()

	mc := &mockCollector{name: "empty", targets: nil}
	mgr := NewManager([]collector.Collector{mc}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/targets", nil)
	w := httptest.NewRecorder()
	mgr.Handler()(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", resp.StatusCode)
	}

	var got []SDTarget
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty array, got %d elements", len(got))
	}
}

func TestHandler_SDTargetFormat(t *testing.T) {
	t.Parallel()

	mc := &mockCollector{
		name: "format",
		targets: []collector.Target{
			{
				URL: "https://example.com/path",
				Labels: map[string]string{
					"namespace":  "production",
					"route_name": "my-ingress",
					"route_kind": "Ingress",
					"host":       "example.com",
					"path":       "/path",
				},
			},
		},
	}

	mgr := NewManager([]collector.Collector{mc}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/targets", nil)
	w := httptest.NewRecorder()
	mgr.Handler()(w, req)

	var got []SDTarget
	if err := json.NewDecoder(w.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 got %d", len(got))
	}

	sd := got[0]
	// targets must be an array of strings.
	if len(sd.Targets) != 1 {
		t.Fatalf("Targets len = %d, want 1", len(sd.Targets))
	}
	if sd.Targets[0] != "https://example.com/path" {
		t.Errorf("Targets[0] = %q", sd.Targets[0])
	}
	// labels must be a flat string→string map.
	if sd.Labels["namespace"] != "production" {
		t.Errorf("Labels[namespace] = %q", sd.Labels["namespace"])
	}
	if sd.Labels["route_kind"] != "Ingress" {
		t.Errorf("Labels[route_kind] = %q", sd.Labels["route_kind"])
	}
}
