package collector

import (
	"context"
	"strings"

	"github.com/r-it-ae/k8s_http_discovery/internal/config"
)

type Target struct {
	URL    string
	Labels map[string]string
}

type Collector interface {
	Name() string
	Collect(ctx context.Context) ([]Target, error)
}

// AnnotationProbePath is the annotation key used to override the monitored
// path on any supported route resource.
const AnnotationProbePath = "k8s-http-discovery.io/probe-path"

// probeOverrides resolves the AnnotationProbePath value for a route.
//
// A value with no "=" (e.g. "/healthz") is a global override: every path
// declared on the route is replaced by it. A value with one or more
// comma-separated "declared-path=override-path" pairs (e.g.
// "/api=/api/healthz,/web=/web/health") only overrides the matching declared
// paths, leaving the others untouched — this lets a single route that fans
// out to multiple backends assign a distinct probe path per backend.
type probeOverrides struct {
	global string
	byPath map[string]string
}

// parseProbeOverrides parses the AnnotationProbePath value off annotations.
func parseProbeOverrides(annotations map[string]string) probeOverrides {
	raw := annotations[AnnotationProbePath]
	if raw == "" {
		return probeOverrides{}
	}
	if !strings.Contains(raw, "=") {
		return probeOverrides{global: raw}
	}

	byPath := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		path, override, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		path, override = strings.TrimSpace(path), strings.TrimSpace(override)
		if path != "" && override != "" {
			byPath[path] = override
		}
	}
	return probeOverrides{byPath: byPath}
}

// resolve returns the probe path to use in place of declaredPath: the global
// override if set, the matching per-path override if any, or declaredPath
// unchanged otherwise.
func (o probeOverrides) resolve(declaredPath string) string {
	if o.global != "" {
		return o.global
	}
	if override, ok := o.byPath[declaredPath]; ok {
		return override
	}
	return declaredPath
}

// AnnotationEnabled is the annotation key used to opt a route resource into
// discovery when config.Config.RequireAnnotation is set.
const AnnotationEnabled = "k8s-http-discovery.io/enabled"

// discoveryAllowed reports whether a route resource should be discovered.
//
// AnnotationEnabled: "false" always excludes the resource, regardless of
// cfg.RequireAnnotation — it's an explicit opt-out. Otherwise, when
// cfg.RequireAnnotation is false (the default), every resource is discovered.
// When true, only resources carrying AnnotationEnabled: "true" are.
func discoveryAllowed(cfg *config.Config, annotations map[string]string) bool {
	v, ok := annotations[AnnotationEnabled]
	if ok && v == "false" {
		return false
	}
	if !cfg.RequireAnnotation {
		return true
	}
	return ok && v == "true"
}
