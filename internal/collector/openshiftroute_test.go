package collector

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"

	"github.com/r-it-ae/k8s_http_discovery/internal/config"
)

func newOpenShiftRoute(namespace, name string, spec map[string]interface{}, annotations map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "route.openshift.io/v1",
			"kind":       "Route",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": spec,
		},
	}
	if len(annotations) > 0 {
		obj.SetAnnotations(annotations)
	}
	return obj
}

func TestOpenShiftRouteCollector_Collect(t *testing.T) {
	t.Parallel()

	routeGVR := schema.GroupVersionResource{
		Group:    "route.openshift.io",
		Version:  "v1",
		Resource: "routes",
	}

	tests := []struct {
		name     string
		obj      *unstructured.Unstructured
		wantURLs []string
		wantLen  int
	}{
		{
			name: "route without TLS uses http scheme",
			obj: newOpenShiftRoute("default", "my-route",
				map[string]interface{}{
					"host": "example.com",
					"path": "/api",
				},
				nil,
			),
			wantURLs: []string{"http://example.com/api"},
			wantLen:  1,
		},
		{
			name: "route with TLS uses https scheme",
			obj: newOpenShiftRoute("default", "tls-route",
				map[string]interface{}{
					"host": "secure.example.com",
					"path": "/",
					"tls": map[string]interface{}{
						"termination": "edge",
					},
				},
				nil,
			),
			wantURLs: []string{"https://secure.example.com/"},
			wantLen:  1,
		},
		{
			name: "route without path defaults to /",
			obj: newOpenShiftRoute("default", "no-path-route",
				map[string]interface{}{
					"host": "nopath.example.com",
				},
				nil,
			),
			wantURLs: []string{"http://nopath.example.com/"},
			wantLen:  1,
		},
		{
			name: "probe-path annotation overrides spec.path",
			obj: newOpenShiftRoute("default", "probe-route",
				map[string]interface{}{
					"host": "probe.example.com",
					"path": "/original",
				},
				map[string]string{"k8s-http-discovery.io/probe-path": "/healthz"},
			),
			wantURLs: []string{"http://probe.example.com/healthz"},
			wantLen:  1,
		},
		{
			name: "probe-path per-path override matching spec.path applies, non-matching leaves path untouched",
			obj: newOpenShiftRoute("default", "keyed-probe-route",
				map[string]interface{}{
					"host": "keyed.example.com",
					"path": "/original",
				},
				map[string]string{"k8s-http-discovery.io/probe-path": "/other=/nope"},
			),
			wantURLs: []string{"http://keyed.example.com/original"},
			wantLen:  1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			dynClient := fake.NewSimpleDynamicClientWithCustomListKinds(scheme,
				map[schema.GroupVersionResource]string{
					routeGVR: "RouteList",
				},
				tc.obj,
			)

			cfg := &config.Config{
				Namespaces:    []string{"default"},
				DefaultScheme: "https",
			}

			c := NewOpenShiftRouteCollector(dynClient, cfg)
			targets, err := c.Collect(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(targets) != tc.wantLen {
				t.Fatalf("expected %d targets, got %d: %v", tc.wantLen, len(targets), targets)
			}
			gotURLs := make([]string, len(targets))
			for i, tgt := range targets {
				gotURLs[i] = tgt.URL
			}
			for _, want := range tc.wantURLs {
				found := false
				for _, got := range gotURLs {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected URL %q not found in %v", want, gotURLs)
				}
			}
			// Verify labels
			for _, tgt := range targets {
				if tgt.Labels["route_kind"] != "OpenShiftRoute" {
					t.Errorf("expected route_kind=OpenShiftRoute, got %q", tgt.Labels["route_kind"])
				}
			}
		})
	}
}

