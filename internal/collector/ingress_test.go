package collector

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/r-it-ae/k8s_http_discovery/internal/config"
)


func TestIngressCollector_Collect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ingresses  []networkingv1.Ingress
		namespaces []string
		wantURLs   []string
		wantLen    int
	}{
		{
			name: "ingress without TLS uses http scheme",
			ingresses: []networkingv1.Ingress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "my-ingress", Namespace: "default"},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								Host: "example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{Path: "/api"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantURLs: []string{"http://example.com/api"},
			wantLen:  1,
		},
		{
			name: "ingress with TLS matching host uses https scheme",
			ingresses: []networkingv1.Ingress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "tls-ingress", Namespace: "default"},
					Spec: networkingv1.IngressSpec{
						TLS: []networkingv1.IngressTLS{
							{Hosts: []string{"secure.example.com"}},
						},
						Rules: []networkingv1.IngressRule{
							{
								Host: "secure.example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{Path: "/"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantURLs: []string{"https://secure.example.com/"},
			wantLen:  1,
		},
		{
			name: "ingress with TLS but non-matching host uses http scheme",
			ingresses: []networkingv1.Ingress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "mixed-ingress", Namespace: "default"},
					Spec: networkingv1.IngressSpec{
						TLS: []networkingv1.IngressTLS{
							{Hosts: []string{"secure.example.com"}},
						},
						Rules: []networkingv1.IngressRule{
							{
								Host: "other.example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{Path: "/plain"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantURLs: []string{"http://other.example.com/plain"},
			wantLen:  1,
		},
		{
			name: "ingress with multiple rules produces multiple targets",
			ingresses: []networkingv1.Ingress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "multi-ingress", Namespace: "default"},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								Host: "a.example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{Path: "/a"},
											{Path: "/b"},
										},
									},
								},
							},
							{
								Host: "c.example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{Path: "/c"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantURLs: []string{
				"http://a.example.com/a",
				"http://a.example.com/b",
				"http://c.example.com/c",
			},
			wantLen: 3,
		},
		{
			name: "ingress rule with empty host is skipped",
			ingresses: []networkingv1.Ingress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "no-host-ingress", Namespace: "default"},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								Host: "", // empty — should be skipped
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{Path: "/skip"},
										},
									},
								},
							},
							{
								Host: "valid.example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{Path: "/keep"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantURLs: []string{"http://valid.example.com/keep"},
			wantLen:  1,
		},
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
		{
			name: "probe-path annotation with per-path overrides only replaces matching paths",
			ingresses: []networkingv1.Ingress{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-backend-ingress",
						Namespace: "default",
						Annotations: map[string]string{
							"k8s-http-discovery.io/probe-path": "/api=/api/healthz,/web=/web/health",
						},
					},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								Host: "example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{
											{Path: "/api"},
											{Path: "/web"},
											{Path: "/unmapped"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantURLs: []string{
				"http://example.com/api/healthz",
				"http://example.com/web/health",
				"http://example.com/unmapped",
			},
			wantLen: 3,
		},
		{
			name:       "empty namespace list collects all namespaces",
			namespaces: []string{},
			ingresses: []networkingv1.Ingress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns1-ingress", Namespace: "ns1"},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								Host: "ns1.example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{{Path: "/"}},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ns2-ingress", Namespace: "ns2"},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								Host: "ns2.example.com",
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: []networkingv1.HTTPIngressPath{{Path: "/"}},
									},
								},
							},
						},
					},
				},
			},
			wantLen: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Build fake client with test ingress objects.
			objs := ingressSliceToRuntimeObjects(tc.ingresses)
			fakeClient := fake.NewSimpleClientset(objs...)
			cfg := &config.Config{
				Namespaces:    tc.namespaces,
				DefaultScheme: "https",
			}
			col := NewIngressCollector(fakeClient, cfg)

			if col.Name() != "ingress" {
				t.Errorf("Name() = %q, want %q", col.Name(), "ingress")
			}

			targets, err := col.Collect(context.Background())
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}

			if len(targets) != tc.wantLen {
				t.Errorf("got %d targets, want %d; targets: %v", len(targets), tc.wantLen, targets)
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

			// Verify labels on each target.
			for _, tgt := range targets {
				for _, key := range []string{"namespace", "route_name", "route_kind", "host", "path"} {
					if tgt.Labels[key] == "" {
						t.Errorf("target %q missing label %q", tgt.URL, key)
					}
				}
				if tgt.Labels["route_kind"] != "Ingress" {
					t.Errorf("target %q: route_kind = %q, want %q", tgt.URL, tgt.Labels["route_kind"], "Ingress")
				}
			}
		})
	}
}

// ingressSliceToRuntimeObjects converts a slice of Ingress to a slice of runtime.Object.
func ingressSliceToRuntimeObjects(ingresses []networkingv1.Ingress) []runtime.Object {
	result := make([]runtime.Object, len(ingresses))
	for i := range ingresses {
		ing := ingresses[i] // capture loop variable
		result[i] = &ing
	}
	return result
}

// urlsOf extracts URLs from a slice of targets for test error messages.
func urlsOf(targets []Target) []string {
	urls := make([]string, len(targets))
	for i, t := range targets {
		urls[i] = t.URL
	}
	return urls
}
