package collector

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/r-it-ae/k8s_http_discovery/internal/config"
)

var apisixrouteGVR = schema.GroupVersionResource{
	Group:    "apisix.apache.org",
	Version:  "v2",
	Resource: "apisixroutes",
}

// ApisixRouteCollector collects Prometheus HTTP SD targets from Apache APISIX ApisixRoute resources.
type ApisixRouteCollector struct {
	dynClient dynamic.Interface
	config    *config.Config
}

// NewApisixRouteCollector creates a new ApisixRouteCollector backed by the given dynamic client.
func NewApisixRouteCollector(dynClient dynamic.Interface, cfg *config.Config) *ApisixRouteCollector {
	return &ApisixRouteCollector{dynClient: dynClient, config: cfg}
}

// Name returns the collector identifier used in configuration.
func (c *ApisixRouteCollector) Name() string { return "apisixroute" }

// Collect lists ApisixRoute resources from configured namespaces and returns one
// Target per (host, path) combination from spec.http[].match.hosts × spec.http[].match.paths.
func (c *ApisixRouteCollector) Collect(ctx context.Context) ([]Target, error) {
	namespaces := c.config.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}

	var targets []Target
	for _, ns := range namespaces {
		list, err := c.dynClient.Resource(apisixrouteGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list apisixroutes in namespace %q: %w", ns, err)
		}
		for i := range list.Items {
			obj := &list.Items[i]
			name := obj.GetName()
			namespace := obj.GetNamespace()

			spec, _ := obj.Object["spec"].(map[string]interface{})
			if spec == nil {
				continue
			}

			httpRules, _ := spec["http"].([]interface{})
			scheme := c.config.DefaultScheme
			probe := probePath(obj.GetAnnotations())

			if probe != "" {
				// Collect all unique hosts across all rules
				seen := make(map[string]bool)
				for _, r := range httpRules {
					rule, _ := r.(map[string]interface{})
					if rule == nil {
						continue
					}
					match, _ := rule["match"].(map[string]interface{})
					if match == nil {
						continue
					}
					if hs, ok := match["hosts"].([]interface{}); ok {
						for _, h := range hs {
							if s, ok := h.(string); ok && s != "" && !seen[s] {
								seen[s] = true
								targets = append(targets, Target{
									URL: fmt.Sprintf("%s://%s%s", scheme, s, probe),
									Labels: map[string]string{
										"namespace":  namespace,
										"route_name": name,
										"route_kind": "ApisixRoute",
										"host":       s,
										"path":       probe,
									},
								})
							}
						}
					}
				}
			} else {
				// Original per-rule fan-out
				for _, r := range httpRules {
					rule, _ := r.(map[string]interface{})
					if rule == nil {
						continue
					}
					match, _ := rule["match"].(map[string]interface{})
					if match == nil {
						continue
					}

					var hosts []string
					if hs, ok := match["hosts"].([]interface{}); ok {
						for _, h := range hs {
							if s, ok := h.(string); ok && s != "" {
								hosts = append(hosts, s)
							}
						}
					}

					var paths []string
					if ps, ok := match["paths"].([]interface{}); ok {
						for _, p := range ps {
							if s, ok := p.(string); ok {
								paths = append(paths, cleanApisixPath(s))
							}
						}
					}

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
			}
		}
	}
	return targets, nil
}

// cleanApisixPath strips trailing wildcard from APISIX path patterns.
// Examples: "/api/*" → "/api/", "/*" → "/", "/exact" → "/exact".
func cleanApisixPath(path string) string {
	return strings.TrimSuffix(path, "*")
}
