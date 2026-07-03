# k8s-http-discovery

Prometheus HTTP SD server for Kubernetes. It watches Ingress, HTTPRoute (Gateway API), ApisixRoute, and OpenShift Route (`route.openshift.io/v1`, kind `Route`) resources across the cluster and exposes every discovered host/path as a target in [Prometheus HTTP SD format](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_sd_config) — so Prometheus (or blackbox_exporter) can probe them without manual scrape config.

## How it works

- A background goroutine lists the configured resource kinds every `CACHE_TTL` and rebuilds an in-memory cache of targets. The HTTP handler always serves from that cache, so requests never block on the Kubernetes API.
- Each `host` × `path` pair found on a route resource becomes one SD target entry, labeled with `namespace`, `route_name`, `route_kind`, `host`, and `path`. `route_kind` is one of `Ingress`, `HTTPRoute`, `ApisixRoute`, or `OpenShiftRoute` (the OpenShift `Route` kind is labeled `OpenShiftRoute` to keep it unambiguous next to the other collectors).
- If a collector fails on a given refresh (e.g. a CRD isn't installed), its error is logged and the other collectors' results are still served — one bad collector doesn't blank the cache.

## Overriding the probe path

By default, every declared host×path on a route resource becomes a target as-is. The `k8s-http-discovery.io/probe-path` annotation lets you point at a different path instead — useful when you want blackbox_exporter to hit a dedicated healthcheck path rather than the route's real paths.

It supports two forms:

- **Global override** — a plain path replaces every path declared on the resource with the same one:

  ```yaml
  annotations:
    k8s-http-discovery.io/probe-path: /healthz
  ```

- **Per-path override** — one or more comma-separated `declared-path=override-path` pairs replace only the matching declared paths; any path not listed is left untouched. This is for a single Ingress/HTTPRoute/ApisixRoute that fans out to multiple backends under different paths, each needing its own healthcheck:

  ```yaml
  annotations:
    k8s-http-discovery.io/probe-path: "/api=/api/healthz,/web=/web/health"
  ```

  With paths `/api`, `/web`, and `/other` declared on the resource, this produces `/api/healthz`, `/web/health`, and `/other` (unchanged) as targets.

For ApisixRoute, the override key is matched against the *cleaned* path (wildcard suffix stripped, e.g. `/api/*` → `/api/`), not the raw declared pattern. OpenShift Route only ever has one path per object, so only a single pair (or the global form) applies.

## Endpoints

| Path | Description |
| --- | --- |
| `GET /targets` | Prometheus HTTP SD target list (JSON). Supports `?kind=Ingress|HTTPRoute|ApisixRoute|OpenShiftRoute` to filter by `route_kind`. |
| `GET /healthz` | Liveness/readiness probe, always `200 OK` once the server is up. |

Example response:

```json
[
  {
    "targets": ["https://example.com/"],
    "labels": {
      "namespace": "default",
      "route_name": "example",
      "route_kind": "Ingress",
      "host": "example.com",
      "path": "/"
    }
  }
]
```

## Configuration

The server is configured entirely through environment variables:

| Variable | Description | Default |
| --- | --- | --- |
| `PORT` | HTTP listen port | `8080` |
| `NAMESPACES` | Comma-separated namespaces to watch; empty means all namespaces (cluster-wide) | `` (all) |
| `COLLECTORS` | Comma-separated collectors to enable: `ingress`, `httproute`, `apisixroute`, `openshiftroute` | `ingress,httproute,apisixroute` |
| `DEFAULT_SCHEME` | Scheme used when a route doesn't otherwise indicate TLS | `https` |
| `CACHE_TTL` | How often the cache is refreshed from the Kubernetes API (Go duration, e.g. `30s`, `1m`) | `30s` |

## Running

### Locally

Requires a working `KUBECONFIG` pointing at a cluster (or run inside a cluster, where it uses the in-cluster config):

```bash
go run ./cmd/
```

### In-cluster (raw manifests)

Plain manifests (RBAC, Deployment, Service) are provided in `deploy/`:

```bash
kubectl apply -f deploy/
```

### In-cluster (Helm)

A Helm chart is published from `charts/k8s-http-discovery`. See [charts/k8s-http-discovery/README.md](charts/k8s-http-discovery/README.md) for installation and configuration details.

## Wiring up Prometheus

Point Prometheus at the `/targets` endpoint via `http_sd_configs`:

```yaml
scrape_configs:
  - job_name: k8s-http-discovery
    http_sd_configs:
      - url: http://k8s-http-discovery.<namespace>.svc:80/targets
```

The Service also carries `prometheus.io/scrape`, `prometheus.io/port`, and `prometheus.io/path` annotations for setups that discover scrape targets from annotated Services directly.

## Development

```bash
go build ./...          # compile
go test ./...           # run all tests
go test ./internal/...  # run unit tests only
```

## RBAC

The server needs cluster-wide read access to the resource kinds it collects (Ingress, HTTPRoute, ApisixRoute, Route) — see `deploy/rbac.yaml` or the chart's `templates/clusterrole.yaml` for the exact rules.
