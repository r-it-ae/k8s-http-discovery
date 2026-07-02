package collector

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/r-it-ae/k8s_http_discovery/internal/config"
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
