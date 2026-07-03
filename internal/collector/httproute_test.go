package collector

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/r-it-ae/k8s_http_discovery/internal/config"
)

// newHTTPRoute builds an unstructured HTTPRoute for testing.
func newHTTPRoute(namespace, name string, hostnames []string, pathValues []string, annotations map[string]string) *unstructured.Unstructured {
	var matches []interface{}
	for _, pv := range pathValues {
		matches = append(matches, map[string]interface{}{
			"path": map[string]interface{}{
				"value": pv,
			},
		})
	}

	var rules []interface{}
	if len(matches) > 0 {
		rules = []interface{}{
			map[string]interface{}{
				"matches": matches,
			},
		}
	}

	hn := make([]interface{}, len(hostnames))
	for i, h := range hostnames {
		hn[i] = h
	}

	spec := map[string]interface{}{
		"hostnames": hn,
	}
	if len(rules) > 0 {
		spec["rules"] = rules
	}

	metadata := map[string]interface{}{
		"name":      name,
		"namespace": namespace,
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata":   metadata,
			"spec":       spec,
		},
	}
	if len(annotations) > 0 {
		obj.SetAnnotations(annotations)
	}
	return obj
}

// httprouteScheme builds a runtime.Scheme that registers HTTPRoute list kind.
func httprouteScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRouteList"},
		&unstructured.UnstructuredList{},
	)
	return scheme
}

func TestHTTPRouteCollector_Collect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		routes    []*unstructured.Unstructured
		wantURLs  []string
		wantLen   int
		wantError bool
	}{
		{
			name: "multiple hostnames cross paths produce N targets",
			routes: []*unstructured.Unstructured{
				newHTTPRoute("default", "my-route",
					[]string{"a.example.com", "b.example.com"},
					[]string{"/api", "/health"},
					nil,
				),
			},
			wantURLs: []string{
				"https://a.example.com/api",
				"https://a.example.com/health",
				"https://b.example.com/api",
				"https://b.example.com/health",
			},
			wantLen: 4,
		},
		{
			name: "HTTPRoute with no match paths falls back to slash",
			routes: []*unstructured.Unstructured{
				newHTTPRoute("default", "no-path-route",
					[]string{"example.com"},
					nil, // no path values
					nil,
				),
			},
			wantURLs: []string{"https://example.com/"},
			wantLen:  1,
		},
		{
			name: "labels are set correctly",
			routes: []*unstructured.Unstructured{
				newHTTPRoute("mynamespace", "myroute",
					[]string{"host.example.com"},
					[]string{"/path"},
					nil,
				),
			},
			wantURLs: []string{"https://host.example.com/path"},
			wantLen:  1,
		},
		{
			name:    "no routes produces empty targets",
			routes:  nil,
			wantLen: 0,
		},
		{
			name: "probe-path annotation overrides host×path fan-out",
			routes: []*unstructured.Unstructured{
				newHTTPRoute("default", "my-route",
					[]string{"a.example.com", "b.example.com"},
					[]string{"/api"},
					map[string]string{"k8s-http-discovery.io/probe-path": "/health"},
				),
			},
			wantURLs: []string{"https://a.example.com/health", "https://b.example.com/health"},
			wantLen:  2,
		},
		{
			name: "probe-path annotation with per-path overrides only replaces matching paths",
			routes: []*unstructured.Unstructured{
				newHTTPRoute("default", "multi-backend-route",
					[]string{"example.com"},
					[]string{"/api", "/web", "/unmapped"},
					map[string]string{"k8s-http-discovery.io/probe-path": "/api=/api/healthz,/web=/web/health"},
				),
			},
			wantURLs: []string{
				"https://example.com/api/healthz",
				"https://example.com/web/health",
				"https://example.com/unmapped",
			},
			wantLen: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := httprouteScheme()
			objs := make([]runtime.Object, len(tc.routes))
			for i, r := range tc.routes {
				objs[i] = r
			}

			dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
				map[schema.GroupVersionResource]string{
					httprouteGVR: "HTTPRouteList",
				},
				objs...,
			)

			cfg := &config.Config{
				Namespaces:    []string{"default", "mynamespace"},
				DefaultScheme: "https",
			}
			col := NewHTTPRouteCollector(dynClient, cfg)

			if col.Name() != "httproute" {
				t.Errorf("Name() = %q, want %q", col.Name(), "httproute")
			}

			targets, err := col.Collect(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("Collect() error = %v, wantError = %v", err, tc.wantError)
			}

			if len(targets) != tc.wantLen {
				t.Errorf("got %d targets, want %d; targets: %v", len(targets), tc.wantLen, urlsOf(targets))
			}

			for _, wantURL := range tc.wantURLs {
				found := false
				for _, tgt := range targets {
					if tgt.URL == wantURL {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected URL %q not found in targets %v", wantURL, urlsOf(targets))
				}
			}

			// Verify all expected labels are present.
			for _, tgt := range targets {
				for _, key := range []string{"namespace", "route_name", "route_kind", "host", "path"} {
					if tgt.Labels[key] == "" {
						t.Errorf("target %q missing label %q", tgt.URL, key)
					}
				}
				if tgt.Labels["route_kind"] != "HTTPRoute" {
					t.Errorf("target %q: route_kind = %q, want %q", tgt.URL, tgt.Labels["route_kind"], "HTTPRoute")
				}
			}
		})
	}
}

func TestHTTPRouteCollector_LabelValues(t *testing.T) {
	t.Parallel()

	route := newHTTPRoute("mynamespace", "myroute", []string{"host.example.com"}, []string{"/path"}, nil)
	scheme := httprouteScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{httprouteGVR: "HTTPRouteList"},
		route,
	)
	cfg := &config.Config{Namespaces: []string{"mynamespace"}, DefaultScheme: "https"}
	col := NewHTTPRouteCollector(dynClient, cfg)

	targets, err := col.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}

	tgt := targets[0]
	checks := map[string]string{
		"namespace":  "mynamespace",
		"route_name": "myroute",
		"route_kind": "HTTPRoute",
		"host":       "host.example.com",
		"path":       "/path",
	}
	for k, want := range checks {
		if got := tgt.Labels[k]; got != want {
			t.Errorf("label %q = %q, want %q", k, got, want)
		}
	}

}
