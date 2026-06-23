# probe-path annotation + kind filter + OpenShift Route Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add annotation-based probe path override, `?kind=` query param filtering, and OpenShift Route support to k8s_http_discovery.

**Architecture:** Three independent features land in three tasks. Task 1 adds the `probePath()` helper and wires it into the three existing collectors. Task 2 adds the OpenShift Route collector (new file, uses the same probe-path helper from Task 1, registered in main.go). Task 3 adds `filterByKind()` in the server handler — pure server concern, no collector changes.

**Tech Stack:** Go 1.23, k8s.io/client-go v0.32.3, k8s.io/apimachinery v0.32.3 (dynamic client for OpenShift Route)

## Global Constraints

- Module: `github.com/Ronan-WeScale/k8s_http_discovery`
- Annotation key: `k8s-http-discovery.io/probe-path` (exact string, no variation)
- Label key for kind filtering: `route_kind` (already set by all collectors)
- `route_kind` values: `"Ingress"`, `"HTTPRoute"`, `"ApisixRoute"`, `"OpenShiftRoute"`
- OpenShift Route GVR: group=`route.openshift.io`, version=`v1`, resource=`routes`
- `openshiftroute` is NOT added to the default `COLLECTORS` env var (avoid errors on non-OpenShift clusters)
- When `?kind=` value is unknown, return `[]` with HTTP 200 (no error)
- All new code in package `collector` or `server` matching existing file conventions
- Commit message style: `feat: <description>` or `fix: <description>`

---

## Task 1: probe-path annotation — Ingress, HTTPRoute, ApisixRoute

**Files:**
- Modify: `internal/collector/collector.go` (add constant + helper)
- Modify: `internal/collector/ingress.go` (`targetsFromIngress` function)
- Modify: `internal/collector/httproute.go` (fan-out loop)
- Modify: `internal/collector/apisixroute.go` (inner fan-out loop)
- Modify: `internal/collector/ingress_test.go` (add probe-path cases)
- Modify: `internal/collector/httproute_test.go` (add probe-path cases)
- Modify: `internal/collector/apisixroute_test.go` (add probe-path cases)

**Interfaces:**
- Produces: `AnnotationProbePath string` constant and `probePath(map[string]string) string` function (package-level, unexported) — consumed by Task 2.

- [ ] **Step 1: Write failing tests for probe-path annotation**

In `internal/collector/ingress_test.go`, add a new test case inside `TestIngressCollector_Collect`:

```go
{
    name: "probe-path annotation replaces all route paths with single target per host",
    ingresses: []networkingv1.Ingress{
        {
            ObjectMeta: metav1.ObjectMeta{
                Name:      "annotated-ingress",
                Namespace: "default",
                Annotations: map[string]string{
                    "k8s-http-discovery.io/probe-path": "/healthz",
                },
            },
            Spec: networkingv1.IngressSpec{
                Rules: []networkingv1.IngressRule{
                    {
                        Host: "example.com",
                        IngressRuleValue: networkingv1.IngressRuleValue{
                            HTTP: &networkingv1.HTTPIngressRuleValue{
                                Paths: []networkingv1.HTTPIngressPath{
                                    {Path: "/api/v1"},
                                    {Path: "/api/v2"},
                                },
                            },
                        },
                    },
                },
            },
        },
    },
    wantURLs: []string{"http://example.com/healthz"},
    wantLen:  1,
},
```

In `internal/collector/httproute_test.go`, add inside the test table:

```go
{
    name: "probe-path annotation overrides host×path fan-out",
    obj: newHTTPRoute("default", "my-route",
        map[string]interface{}{
            "hostnames": []interface{}{"a.example.com", "b.example.com"},
            "rules": []interface{}{
                map[string]interface{}{
                    "matches": []interface{}{
                        map[string]interface{}{
                            "path": map[string]interface{}{"value": "/api"},
                        },
                    },
                },
            },
        },
        map[string]string{"k8s-http-discovery.io/probe-path": "/health"},
    ),
    wantURLs: []string{"https://a.example.com/health", "https://b.example.com/health"},
    wantLen:  2,
},
```

In `internal/collector/apisixroute_test.go`, add inside the test table:

```go
{
    name: "probe-path annotation overrides all rule paths",
    obj: newApisixRoute("default", "my-route",
        []interface{}{
            map[string]interface{}{
                "match": map[string]interface{}{
                    "hosts": []interface{}{"x.com"},
                    "paths": []interface{}{"/api/*", "/v2/*"},
                },
            },
        },
        map[string]string{"k8s-http-discovery.io/probe-path": "/ping"},
    ),
    wantURLs: []string{"https://x.com/ping"},
    wantLen:  1,
},
```

> Note: if `newHTTPRoute` or `newApisixRoute` helpers don't accept annotations as a parameter yet, update their signature to accept `annotations map[string]string` and set `obj.SetAnnotations(annotations)`.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/collector/... -run "probe-path" -v
```

Expected: FAIL — `AnnotationProbePath` undefined or probe path not applied.

- [ ] **Step 3: Add the constant and helper to collector.go**

In `internal/collector/collector.go`, add after the `Collector` interface:

```go
// AnnotationProbePath is the annotation key used to override the monitored
// path on any supported route resource.
const AnnotationProbePath = "k8s-http-discovery.io/probe-path"

// probePath returns the probe path annotation value if set, else empty string.
// A non-empty result means the collector should emit one target per host
// pointing to this path instead of the route's declared paths.
func probePath(annotations map[string]string) string {
	return annotations[AnnotationProbePath]
}
```

- [ ] **Step 4: Update `targetsFromIngress` in ingress.go**

Replace the entire `targetsFromIngress` function:

```go
func targetsFromIngress(ing *networkingv1.Ingress) []Target {
	tlsHosts := make(map[string]bool)
	for _, tls := range ing.Spec.TLS {
		for _, h := range tls.Hosts {
			tlsHosts[h] = true
		}
	}

	probe := probePath(ing.Annotations)

	var targets []Target
	for _, rule := range ing.Spec.Rules {
		if rule.Host == "" {
			continue
		}
		scheme := "http"
		if len(ing.Spec.TLS) > 0 && tlsHosts[rule.Host] {
			scheme = "https"
		}

		if probe != "" {
			targets = append(targets, Target{
				URL: fmt.Sprintf("%s://%s%s", scheme, rule.Host, probe),
				Labels: map[string]string{
					"namespace":  ing.Namespace,
					"route_name": ing.Name,
					"route_kind": "Ingress",
					"host":       rule.Host,
					"path":       probe,
				},
			})
			continue
		}

		if rule.HTTP == nil {
			targets = append(targets, Target{
				URL: fmt.Sprintf("%s://%s/", scheme, rule.Host),
				Labels: map[string]string{
					"namespace":  ing.Namespace,
					"route_name": ing.Name,
					"route_kind": "Ingress",
					"host":       rule.Host,
					"path":       "/",
				},
			})
			continue
		}

		for _, p := range rule.HTTP.Paths {
			path := p.Path
			if path == "" {
				path = "/"
			}
			targets = append(targets, Target{
				URL: fmt.Sprintf("%s://%s%s", scheme, rule.Host, path),
				Labels: map[string]string{
					"namespace":  ing.Namespace,
					"route_name": ing.Name,
					"route_kind": "Ingress",
					"host":       rule.Host,
					"path":       path,
				},
			})
		}
	}
	return targets
}
```

- [ ] **Step 5: Update HTTPRoute fan-out in httproute.go**

Replace the fan-out block at the bottom of the `for i := range list.Items` loop (after `if len(paths) == 0 { paths = []string{"/"} }`):

```go
			scheme := c.config.DefaultScheme
			probe := probePath(obj.GetAnnotations())
			if probe != "" {
				for _, host := range hostnames {
					targets = append(targets, Target{
						URL: fmt.Sprintf("%s://%s%s", scheme, host, probe),
						Labels: map[string]string{
							"namespace":  namespace,
							"route_name": name,
							"route_kind": "HTTPRoute",
							"host":       host,
							"path":       probe,
						},
					})
				}
			} else {
				for _, host := range hostnames {
					for _, path := range paths {
						targets = append(targets, Target{
							URL: fmt.Sprintf("%s://%s%s", scheme, host, path),
							Labels: map[string]string{
								"namespace":  namespace,
								"route_name": name,
								"route_kind": "HTTPRoute",
								"host":       host,
								"path":       path,
							},
						})
					}
				}
			}
```

- [ ] **Step 6: Update ApisixRoute fan-out in apisixroute.go**

Replace the inner `for _, host := range hosts { for _, path := range paths { ... } }` block (inside `for _, r := range httpRules`):

```go
				probe := probePath(obj.GetAnnotations())
				if probe != "" {
					for _, host := range hosts {
						targets = append(targets, Target{
							URL: fmt.Sprintf("%s://%s%s", scheme, host, probe),
							Labels: map[string]string{
								"namespace":  namespace,
								"route_name": name,
								"route_kind": "ApisixRoute",
								"host":       host,
								"path":       probe,
							},
						})
					}
				} else {
					for _, host := range hosts {
						for _, path := range paths {
							targets = append(targets, Target{
								URL: fmt.Sprintf("%s://%s%s", scheme, host, path),
								Labels: map[string]string{
									"namespace":  namespace,
									"route_name": name,
									"route_kind": "ApisixRoute",
									"host":       host,
									"path":       path,
								},
							})
						}
					}
				}
```

- [ ] **Step 7: Run all tests**

```bash
go test ./internal/collector/... -v -race
```

Expected: all tests pass, including the new probe-path cases.

- [ ] **Step 8: Commit**

```bash
git add internal/collector/
git commit -m "feat: probe-path annotation override for ingress, httproute, apisixroute"
```

---

## Task 2: OpenShift Route collector

**Files:**
- Create: `internal/collector/openshiftroute.go`
- Create: `internal/collector/openshiftroute_test.go`
- Modify: `cmd/main.go` (add `"openshiftroute"` case to the switch)

**Interfaces:**
- Consumes: `AnnotationProbePath`, `probePath()` from Task 1 (same package, same file)
- Consumes: `collector.Collector` interface, `config.Config`
- Produces: `NewOpenShiftRouteCollector(dynClient dynamic.Interface, cfg *config.Config) *OpenShiftRouteCollector`

- [ ] **Step 1: Write failing tests**

Create `internal/collector/openshiftroute_test.go`:

```go
package collector

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"

	"github.com/Ronan-WeScale/k8s_http_discovery/internal/config"
)

func newOpenShiftRoute(namespace, name string, spec map[string]interface{}, annotations map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "route.openshift.io/v1",
			"kind":       "Route",
			"metadata": map[string]interface{}{
				"name":        name,
				"namespace":   namespace,
				"annotations": func() map[string]interface{} {
					out := make(map[string]interface{})
					for k, v := range annotations {
						out[k] = v
					}
					return out
				}(),
			},
			"spec": spec,
		},
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/collector/... -run "OpenShiftRoute" -v
```

Expected: FAIL — `NewOpenShiftRouteCollector` undefined.

- [ ] **Step 3: Implement the collector**

Create `internal/collector/openshiftroute.go`:

```go
package collector

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/Ronan-WeScale/k8s_http_discovery/internal/config"
)

var openshiftrouteGVR = schema.GroupVersionResource{
	Group:    "route.openshift.io",
	Version:  "v1",
	Resource: "routes",
}

// OpenShiftRouteCollector collects Prometheus HTTP SD targets from OpenShift Route resources.
type OpenShiftRouteCollector struct {
	dynClient dynamic.Interface
	config    *config.Config
}

// NewOpenShiftRouteCollector creates a new OpenShiftRouteCollector backed by the given dynamic client.
func NewOpenShiftRouteCollector(dynClient dynamic.Interface, cfg *config.Config) *OpenShiftRouteCollector {
	return &OpenShiftRouteCollector{dynClient: dynClient, config: cfg}
}

// Name returns the collector identifier used in configuration.
func (c *OpenShiftRouteCollector) Name() string { return "openshiftroute" }

// Collect lists Route resources from configured namespaces and returns one Target per Route.
// Scheme is determined by the presence of spec.tls. The probe-path annotation overrides spec.path.
func (c *OpenShiftRouteCollector) Collect(ctx context.Context) ([]Target, error) {
	namespaces := c.config.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}

	var targets []Target
	for _, ns := range namespaces {
		list, err := c.dynClient.Resource(openshiftrouteGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list openshiftroutes in namespace %q: %w", ns, err)
		}
		for i := range list.Items {
			obj := &list.Items[i]
			name := obj.GetName()
			namespace := obj.GetNamespace()

			spec, _ := obj.Object["spec"].(map[string]interface{})
			if spec == nil {
				continue
			}

			host, _ := spec["host"].(string)
			if host == "" {
				continue
			}

			scheme := "http"
			if _, ok := spec["tls"].(map[string]interface{}); ok {
				scheme = "https"
			}

			path, _ := spec["path"].(string)
			if path == "" {
				path = "/"
			}

			if p := probePath(obj.GetAnnotations()); p != "" {
				path = p
			}

			targets = append(targets, Target{
				URL: fmt.Sprintf("%s://%s%s", scheme, host, path),
				Labels: map[string]string{
					"namespace":  namespace,
					"route_name": name,
					"route_kind": "OpenShiftRoute",
					"host":       host,
					"path":       path,
				},
			})
		}
	}
	return targets, nil
}
```

- [ ] **Step 4: Register the collector in cmd/main.go**

In the `switch name` block, add after the `"apisixroute"` case:

```go
		case "openshiftroute":
			collectors = append(collectors, collector.NewOpenShiftRouteCollector(dynClient, cfg))
```

- [ ] **Step 5: Run all tests**

```bash
go test ./... -v -race
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/collector/openshiftroute.go internal/collector/openshiftroute_test.go cmd/main.go
git commit -m "feat: OpenShift Route collector with probe-path annotation support"
```

---

## Task 3: `?kind=` query param filter on /targets

**Files:**
- Modify: `internal/server/handler.go` (add `filterByKind`, update `Handler`)
- Modify: `internal/server/handler_test.go` (add 3 new test cases)

**Interfaces:**
- No new exported symbols. `filterByKind` is unexported.

- [ ] **Step 1: Write failing tests**

In `internal/server/handler_test.go`, add three new test functions:

```go
func TestHandler_KindFilter_Ingress(t *testing.T) {
	t.Parallel()

	mc := &mockCollector{
		name: "mixed",
		targets: []collector.Target{
			{URL: "https://a.com/", Labels: map[string]string{"route_kind": "Ingress", "host": "a.com", "path": "/"}},
			{URL: "https://b.com/", Labels: map[string]string{"route_kind": "HTTPRoute", "host": "b.com", "path": "/"}},
		},
	}
	mgr := NewManager([]collector.Collector{mc}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	req := httptest.NewRequest(http.MethodGet, "/targets?kind=Ingress", nil)
	w := httptest.NewRecorder()
	mgr.Handler()(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got []SDTarget
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 target, got %d: %v", len(got), got)
	}
	if got[0].Labels["route_kind"] != "Ingress" {
		t.Errorf("expected route_kind=Ingress, got %q", got[0].Labels["route_kind"])
	}
}

func TestHandler_KindFilter_UnknownKind(t *testing.T) {
	t.Parallel()

	mc := &mockCollector{
		name:    "ingress",
		targets: []collector.Target{
			{URL: "https://a.com/", Labels: map[string]string{"route_kind": "Ingress"}},
		},
	}
	mgr := NewManager([]collector.Collector{mc}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	req := httptest.NewRequest(http.MethodGet, "/targets?kind=Unknown", nil)
	w := httptest.NewRecorder()
	mgr.Handler()(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var got []SDTarget
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty array, got %d targets", len(got))
	}
}

func TestHandler_NoKindFilter_ReturnsAll(t *testing.T) {
	t.Parallel()

	mc := &mockCollector{
		name: "mixed",
		targets: []collector.Target{
			{URL: "https://a.com/", Labels: map[string]string{"route_kind": "Ingress"}},
			{URL: "https://b.com/", Labels: map[string]string{"route_kind": "HTTPRoute"}},
		},
	}
	mgr := NewManager([]collector.Collector{mc}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	req := httptest.NewRequest(http.MethodGet, "/targets", nil)
	w := httptest.NewRecorder()
	mgr.Handler()(w, req)

	var got []SDTarget
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/server/... -run "KindFilter" -v
```

Expected: FAIL — `?kind=` parameter is ignored, returns all targets.

- [ ] **Step 3: Add filterByKind and update Handler**

In `internal/server/handler.go`, add after the `refresh` function:

```go
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
```

Replace the `Handler()` method body with:

```go
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
```

- [ ] **Step 4: Run all tests**

```bash
go test ./... -v -race
```

Expected: all tests pass including the 3 new KindFilter tests.

- [ ] **Step 5: Commit**

```bash
git add internal/server/handler.go internal/server/handler_test.go
git commit -m "feat: filter /targets by ?kind= query param"
```

---

## Self-Review Checklist

- [x] **probe-path annotation** → Tasks 1 (Ingress/HTTPRoute/ApisixRoute) + Task 2 (OpenShiftRoute) ✓
- [x] **`?kind=` filter** → Task 3 ✓
- [x] **OpenShift Route collector** → Task 2 ✓
- [x] **`openshiftroute` not in default COLLECTORS** → handled by not modifying `config.go` default ✓
- [x] **Unknown `?kind=`** → `filterByKind` returns empty slice → JSON `[]`, HTTP 200 ✓
- [x] **`probePath()` helper name** consistent across all tasks ✓
- [x] **`route_kind="OpenShiftRoute"`** consistent between collector label and kind filter ✓
- [x] No placeholders, no TODOs
