// Package collector provides Kubernetes resource collectors that produce
// Prometheus HTTP SD targets.
package collector

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/r-it-ae/k8s_http_discovery/internal/config"
)

// IngressCollector collects Prometheus HTTP SD targets from Kubernetes Ingress resources.
type IngressCollector struct {
	client kubernetes.Interface
	config *config.Config
}

// NewIngressCollector creates a new IngressCollector backed by the given Kubernetes client.
func NewIngressCollector(client kubernetes.Interface, cfg *config.Config) *IngressCollector {
	return &IngressCollector{client: client, config: cfg}
}

// Name returns the collector identifier used in configuration.
func (c *IngressCollector) Name() string { return "ingress" }

// Collect lists Ingress resources from configured namespaces and returns one
// Target per (host, path) combination.
func (c *IngressCollector) Collect(ctx context.Context) ([]Target, error) {
	namespaces := c.config.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}

	var targets []Target
	for _, ns := range namespaces {
		list, err := c.client.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list ingresses in namespace %q: %w", ns, err)
		}
		for i := range list.Items {
			ing := &list.Items[i]
			if !discoveryAllowed(c.config, ing.Annotations) {
				continue
			}
			targets = append(targets, targetsFromIngress(ing)...)
		}
	}
	return targets, nil
}

// targetsFromIngress converts a single Ingress to a slice of Targets.
func targetsFromIngress(ing *networkingv1.Ingress) []Target {
	tlsHosts := make(map[string]bool)
	for _, tls := range ing.Spec.TLS {
		for _, h := range tls.Hosts {
			tlsHosts[h] = true
		}
	}

	overrides := parseProbeOverrides(ing.Annotations)

	var targets []Target
	for _, rule := range ing.Spec.Rules {
		if rule.Host == "" {
			continue
		}
		scheme := "http"
		if len(ing.Spec.TLS) > 0 && tlsHosts[rule.Host] {
			scheme = "https"
		}

		if overrides.global != "" {
			targets = append(targets, Target{
				URL: fmt.Sprintf("%s://%s%s", scheme, rule.Host, overrides.global),
				Labels: map[string]string{
					"namespace":  ing.Namespace,
					"route_name": ing.Name,
					"route_kind": "Ingress",
					"host":       rule.Host,
					"path":       overrides.global,
				},
			})
			continue
		}

		if rule.HTTP == nil {
			path := overrides.resolve("/")
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
			continue
		}

		for _, p := range rule.HTTP.Paths {
			path := p.Path
			if path == "" {
				path = "/"
			}
			path = overrides.resolve(path)
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
