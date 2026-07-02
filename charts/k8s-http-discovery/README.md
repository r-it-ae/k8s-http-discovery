# k8s-http-discovery

Helm chart for [k8s-http-discovery](https://github.com/r-it-ae/k8s_http_discovery), a Prometheus HTTP SD server for Kubernetes that discovers HTTP targets from Ingress, HTTPRoute (Gateway API), and ApisixRoute custom resources.

## Installation

```bash
helm repo add k8s-http-discovery https://r-it-ae.github.io/k8s_http_discovery/
helm repo update
helm install k8s-http-discovery k8s-http-discovery/k8s-http-discovery
```

## Uninstallation

```bash
helm uninstall k8s-http-discovery
```

## Configuration

| Parameter                     | Description                                                          | Default                                       |
| ------------------------------ | ---------------------------------------------------------------------- | ------------------------------------------------ |
| `replicaCount`                | Number of replicas                                                    | `1`                                            |
| `image.repository`            | Image repository                                                      | `ghcr.io/r-it-ae/k8s-http-discovery`     |
| `image.pullPolicy`            | Image pull policy                                                     | `IfNotPresent`                                 |
| `image.tag`                   | Image tag (defaults to `.Chart.AppVersion` if empty)                  | `""`                                           |
| `serviceAccount.create`       | Whether to create a ServiceAccount                                    | `true`                                         |
| `serviceAccount.name`         | ServiceAccount name (generated if empty)                              | `""`                                           |
| `rbac.create`                 | Whether to create the ClusterRole/ClusterRoleBinding                  | `true`                                         |
| `podSecurityContext`          | Pod-level security context                                            | `runAsNonRoot: true, runAsUser: 65532`         |
| `service.type`                | Kubernetes Service type                                               | `ClusterIP`                                    |
| `service.port`                | Service port                                                          | `80`                                           |
| `service.targetPort`          | Container port targeted by the Service                                | `8080`                                         |
| `service.annotations`         | Extra annotations added to the Service                                | Prometheus scrape annotations                  |
| `config.port`                 | Port the server listens on (`PORT`)                                   | `"8080"`                                       |
| `config.namespaces`           | Comma-separated namespaces to watch, empty = all (`NAMESPACES`)       | `""`                                           |
| `config.collectors`           | Comma-separated collectors to enable (`COLLECTORS`)                   | `"ingress,httproute,apisixroute"`              |
| `config.defaultScheme`        | Default scheme for targets without one (`DEFAULT_SCHEME`)             | `"https"`                                      |
| `config.cacheTTL`             | Cache refresh interval (`CACHE_TTL`)                                  | `"30s"`                                        |
| `resources`                   | Pod resource requests/limits                                          | `requests: 10m/32Mi, limits: 100m/128Mi`       |
| `livenessProbe`               | Liveness probe configuration                                          | HTTP GET `/healthz` on port `8080`             |
| `readinessProbe`              | Readiness probe configuration                                         | HTTP GET `/healthz` on port `8080`             |

See `values.yaml` for the full set of defaults.

## Prometheus scrape configuration

Once deployed, add the service as an HTTP SD source in your Prometheus config:

```yaml
scrape_configs:
  - job_name: k8s-http-discovery
    http_sd_configs:
      - url: http://k8s-http-discovery.<namespace>.svc:80/targets
```
