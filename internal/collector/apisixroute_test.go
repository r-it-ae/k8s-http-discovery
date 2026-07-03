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

// newApisixRoute builds an unstructured ApisixRoute for testing.
// httpRules is a slice of (hosts, paths) pairs representing spec.http entries.
func newApisixRoute(namespace, name string, httpRules []apisixHTTPRule, annotations map[string]string) *unstructured.Unstructured {
	rules := make([]interface{}, len(httpRules))
	for i, r := range httpRules {
		hosts := make([]interface{}, len(r.hosts))
		for j, h := range r.hosts {
			hosts[j] = h
		}
		paths := make([]interface{}, len(r.paths))
		for j, p := range r.paths {
			paths[j] = p
		}
		rules[i] = map[string]interface{}{
			"match": map[string]interface{}{
				"hosts": hosts,
				"paths": paths,
			},
		}
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apisix.apache.org/v2",
			"kind":       "ApisixRoute",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"http": rules,
			},
		},
	}
	if len(annotations) > 0 {
		obj.SetAnnotations(annotations)
	}
	return obj
}

type apisixHTTPRule struct {
	hosts []string
	paths []string
}

// apisixScheme builds a runtime.Scheme that registers ApisixRoute list kind.
func apisixScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "apisix.apache.org", Version: "v2", Kind: "ApisixRoute"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "apisix.apache.org", Version: "v2", Kind: "ApisixRouteList"},
		&unstructured.UnstructuredList{},
	)
	return scheme
}

func TestApisixRouteCollector_Collect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		routes   []*unstructured.Unstructured
		wantURLs []string
		wantLen  int
	}{
		{
			name: "path /api/* is cleaned to /api/",
			routes: []*unstructured.Unstructured{
				newApisixRoute("default", "api-route", []apisixHTTPRule{
					{hosts: []string{"example.com"}, paths: []string{"/api/*"}},
				}, nil),
			},
			wantURLs: []string{"https://example.com/api/"},
			wantLen:  1,
		},
		{
			name: "path /* is cleaned to /",
			routes: []*unstructured.Unstructured{
				newApisixRoute("default", "root-route", []apisixHTTPRule{
					{hosts: []string{"example.com"}, paths: []string{"/*"}},
				}, nil),
			},
			wantURLs: []string{"https://example.com/"},
			wantLen:  1,
		},
		{
			name: "exact path without wildcard is preserved",
			routes: []*unstructured.Unstructured{
				newApisixRoute("default", "exact-route", []apisixHTTPRule{
					{hosts: []string{"example.com"}, paths: []string{"/exact"}},
				}, nil),
			},
			wantURLs: []string{"https://example.com/exact"},
			wantLen:  1,
		},
		{
			name: "multiple hosts cross multiple paths produce N targets",
			routes: []*unstructured.Unstructured{
				newApisixRoute("default", "multi-route", []apisixHTTPRule{
					{
						hosts: []string{"a.example.com", "b.example.com"},
						paths: []string{"/api/*", "/health"},
					},
				}, nil),
			},
			wantURLs: []string{
				"https://a.example.com/api/",
				"https://a.example.com/health",
				"https://b.example.com/api/",
				"https://b.example.com/health",
			},
			wantLen: 4,
		},
		{
			name: "multiple spec.http rules are each expanded",
			routes: []*unstructured.Unstructured{
				newApisixRoute("default", "two-rules", []apisixHTTPRule{
					{hosts: []string{"a.example.com"}, paths: []string{"/api/*"}},
					{hosts: []string{"b.example.com"}, paths: []string{"/other"}},
				}, nil),
			},
			wantURLs: []string{
				"https://a.example.com/api/",
				"https://b.example.com/other",
			},
			wantLen: 2,
		},
		{
			name:    "no routes produces empty result",
			routes:  nil,
			wantLen: 0,
		},
		{
			name: "probe-path annotation overrides all rule paths",
			routes: []*unstructured.Unstructured{
				newApisixRoute("default", "my-route", []apisixHTTPRule{
					{
						hosts: []string{"x.com"},
						paths: []string{"/api/*", "/v2/*"},
					},
				}, map[string]string{"k8s-http-discovery.io/probe-path": "/ping"}),
			},
			wantURLs: []string{"https://x.com/ping"},
			wantLen:  1,
		},
		{
			name: "probe-path with multiple rules sharing same host emits one target per unique host",
			routes: []*unstructured.Unstructured{
				newApisixRoute("default", "multi-rule", []apisixHTTPRule{
					{
						hosts: []string{"x.com"},
						paths: []string{"/api/*"},
					},
					{
						hosts: []string{"x.com"},
						paths: []string{"/v2/*"},
					},
				}, map[string]string{"k8s-http-discovery.io/probe-path": "/ping"}),
			},
			wantURLs: []string{"https://x.com/ping"},
			wantLen:  1,
		},
		{
			name: "probe-path annotation with per-path overrides only replaces matching cleaned paths",
			routes: []*unstructured.Unstructured{
				newApisixRoute("default", "multi-backend-route", []apisixHTTPRule{
					{
						hosts: []string{"x.com"},
						paths: []string{"/api/*", "/web/*", "/unmapped"},
					},
				}, map[string]string{"k8s-http-discovery.io/probe-path": "/api/=/api/healthz,/web/=/web/health"}),
			},
			wantURLs: []string{
				"https://x.com/api/healthz",
				"https://x.com/web/health",
				"https://x.com/unmapped",
			},
			wantLen: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := apisixScheme()
			objs := make([]runtime.Object, len(tc.routes))
			for i, r := range tc.routes {
				objs[i] = r
			}
			dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
				map[schema.GroupVersionResource]string{
					apisixrouteGVR: "ApisixRouteList",
				},
				objs...,
			)

			cfg := &config.Config{
				Namespaces:    []string{"default"},
				DefaultScheme: "https",
			}
			col := NewApisixRouteCollector(dynClient, cfg)

			if col.Name() != "apisixroute" {
				t.Errorf("Name() = %q, want %q", col.Name(), "apisixroute")
			}

			targets, err := col.Collect(context.Background())
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
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
				if tgt.Labels["route_kind"] != "ApisixRoute" {
					t.Errorf("target %q: route_kind = %q, want %q", tgt.URL, tgt.Labels["route_kind"], "ApisixRoute")
				}
			}
		})
	}
}

func TestCleanApisixPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"/api/*", "/api/"},
		{"/*", "/"},
		{"/exact", "/exact"},
		{"/", "/"},
		{"/nested/path/*", "/nested/path/"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := cleanApisixPath(tc.input)
			if got != tc.want {
				t.Errorf("cleanApisixPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
