package collector

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/r-it-ae/k8s_http_discovery/internal/config"
)

var httprouteGVR = schema.GroupVersionResource{
	Group:    "gateway.networking.k8s.io",
	Version:  "v1",
	Resource: "httproutes",
}

// HTTPRouteCollector collects Prometheus HTTP SD targets from Gateway API HTTPRoute resources.
type HTTPRouteCollector struct {
	dynClient dynamic.Interface
	config    *config.Config
}

// NewHTTPRouteCollector creates a new HTTPRouteCollector backed by the given dynamic client.
func NewHTTPRouteCollector(dynClient dynamic.Interface, cfg *config.Config) *HTTPRouteCollector {
	return &HTTPRouteCollector{dynClient: dynClient, config: cfg}
}

// Name returns the collector identifier used in configuration.
func (c *HTTPRouteCollector) Name() string { return "httproute" }

// Collect lists HTTPRoute resources from configured namespaces and returns one
// Target per (hostname, path) combination.
func (c *HTTPRouteCollector) Collect(ctx context.Context) ([]Target, error) {
	namespaces := c.config.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}

	var targets []Target
	for _, ns := range namespaces {
		list, err := c.dynClient.Resource(httprouteGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list httproutes in namespace %q: %w", ns, err)
		}
		for i := range list.Items {
			obj := &list.Items[i]
			if !discoveryAllowed(c.config, obj.GetAnnotations()) {
				continue
			}
			name := obj.GetName()
			namespace := obj.GetNamespace()

			spec, _ := obj.Object["spec"].(map[string]interface{})
			if spec == nil {
				continue
			}

			// Collect hostnames.
			var hostnames []string
			if hn, ok := spec["hostnames"].([]interface{}); ok {
				for _, h := range hn {
					if s, ok := h.(string); ok && s != "" {
						hostnames = append(hostnames, s)
					}
				}
			}
			if len(hostnames) == 0 {
				continue
			}

			// Collect path values from spec.rules[].matches[].path.value.
			var paths []string
			if rules, ok := spec["rules"].([]interface{}); ok {
				for _, r := range rules {
					rule, _ := r.(map[string]interface{})
					if rule == nil {
						continue
					}
					matches, _ := rule["matches"].([]interface{})
					for _, m := range matches {
						match, _ := m.(map[string]interface{})
						if match == nil {
							continue
						}
						pathObj, _ := match["path"].(map[string]interface{})
						if pathObj == nil {
							continue
						}
						if v, ok := pathObj["value"].(string); ok && v != "" {
							paths = append(paths, v)
						}
					}
				}
			}
			if len(paths) == 0 {
				paths = []string{"/"}
			}

			scheme := c.config.DefaultScheme
			overrides := parseProbeOverrides(obj.GetAnnotations())
			if overrides.global != "" {
				for _, host := range hostnames {
					targets = append(targets, Target{
						URL: fmt.Sprintf("%s://%s%s", scheme, host, overrides.global),
						Labels: map[string]string{
							"namespace":  namespace,
							"route_name": name,
							"route_kind": "HTTPRoute",
							"host":       host,
							"path":       overrides.global,
						},
					})
				}
			} else {
				for _, host := range hostnames {
					for _, path := range paths {
						path := overrides.resolve(path)
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
		}
	}
	return targets, nil
}
