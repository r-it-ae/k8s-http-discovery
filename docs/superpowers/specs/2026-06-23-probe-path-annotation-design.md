# Design : probe-path annotation + filtre kind

**Date:** 2026-06-23
**Scope:** Deux features complémentaires pour l'endpoint HTTP SD :
1. Annotation `k8s-http-discovery.io/probe-path` pour surcharger le path de monitoring.
2. Query param `?kind=` pour filtrer les targets par type de ressource.

---

## Contexte et motivation

Le service discovery génère aujourd'hui une target par couple (host, path) déclaré dans la route. Pour Blackbox Exporter, l'objectif est de sonder un endpoint de santé (`/healthz`, `/health`, `/ping`, etc.) et non les paths fonctionnels de l'application. Sans mécanisme de surcharge, il faut relabeler dans la config Prometheus ou accepter des URLs non pertinentes.

---

## Comportement cible

### Sans annotation (comportement inchangé)

Chaque (host, path) déclaré dans la CR devient une target :
```
https://example.com/api/v1
https://example.com/api/v2
```

### Avec annotation

```yaml
metadata:
  annotations:
    k8s-http-discovery.io/probe-path: /healthz
```

Une seule target par host, pointant vers le probe path :
```
https://example.com/healthz
```

Le label `path` reflète le probe path (`/healthz`). Les paths déclarés dans la route sont ignorés.

---

## Spécification

### Annotation

| Clé | Valeur | Exemple |
|-----|--------|---------|
| `k8s-http-discovery.io/probe-path` | Path absolu | `/healthz`, `/health`, `/ping` |

- La valeur doit commencer par `/`. Si ce n'est pas le cas, le path est utilisé tel quel (pas de validation stricte — l'opérateur est responsable).
- L'annotation est optionnelle. Absente = comportement inchangé.

### Ressources supportées

- `networking.k8s.io/v1` Ingress
- `gateway.networking.k8s.io/v1` HTTPRoute
- `apisix.apache.org/v2` ApisixRoute

### Logique de résolution dans chaque collecteur

```
si annotation "k8s-http-discovery.io/probe-path" présente:
    pour chaque host de la ressource:
        émettre Target{URL: scheme://host/<probe-path>, Labels{..., path: <probe-path>}}
sinon:
    comportement actuel (fan-out host × path)
```

---

## Implémentation

### `internal/collector/collector.go`

Ajouter la constante et le helper partagé :

```go
const AnnotationProbePath = "k8s-http-discovery.io/probe-path"

// probePath returns the probe-path annotation value if set, else empty string.
func probePath(annotations map[string]string) string {
    return annotations[AnnotationProbePath]
}
```

### `internal/collector/ingress.go`

Dans `targetsFromIngress`, lire `ing.Annotations` via le helper. Si probe path défini, émettre une target par host (en remplaçant le fan-out par host×path existant).

### `internal/collector/httproute.go`

Dans la boucle de collecte, lire `obj.GetAnnotations()`. Si probe path défini, émettre une target par hostname.

### `internal/collector/apisixroute.go`

Idem : lire `obj.GetAnnotations()`. Si probe path défini, émettre une target par host.

---

## Tests

Chaque fichier de test collecteur ajoute deux cas :

1. **Avec annotation** — ressource avec `k8s-http-discovery.io/probe-path: /healthz` et plusieurs paths déclarés → une seule target par host avec URL `scheme://host/healthz` et label `path=/healthz`.
2. **Sans annotation** — vérification que le comportement existant est inchangé (cas déjà couverts, mais à confirmer qu'ils passent toujours).

---

## Feature 2 : filtre `?kind=` sur l'endpoint `/targets`

### Comportement

Le query param `kind` filtre les targets retournées par type de ressource :

| URL | Résultat |
|-----|---------|
| `/targets` | Toutes les targets (comportement inchangé) |
| `/targets?kind=Ingress` | Uniquement les targets dont `route_kind=Ingress` |
| `/targets?kind=HTTPRoute` | Uniquement les targets dont `route_kind=HTTPRoute` |
| `/targets?kind=ApisixRoute` | Uniquement les targets dont `route_kind=ApisixRoute` |
| `/targets?kind=Unknown` | `[]` (tableau vide, pas d'erreur) |

- Un seul kind par requête.
- Valeur non reconnue → réponse 200 avec `[]`.
- Pas de modification du cache — le filtrage se fait au moment de servir la réponse.

### Implémentation

Dans `internal/server/handler.go`, le `Handler()` lit `r.URL.Query().Get("kind")`. Si non vide, filtre le slice du cache :

```go
kind := r.URL.Query().Get("kind")
result := cached
if kind != "" {
    result = filterByKind(cached, kind)
}
```

```go
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

### Tests

Dans `internal/server/handler_test.go` :

1. Sans `?kind=` → toutes les targets retournées.
2. `?kind=Ingress` sur un cache mixte (Ingress + HTTPRoute) → seules les targets Ingress.
3. `?kind=Unknown` → `[]` JSON, status 200.

### Exemple de config Prometheus

```yaml
scrape_configs:
  - job_name: blackbox_ingress
    metrics_path: /probe
    params:
      module: [http_2xx]
    http_sd_configs:
      - url: http://k8s-http-discovery/targets?kind=Ingress
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      # ...

  - job_name: blackbox_httproute
    http_sd_configs:
      - url: http://k8s-http-discovery/targets?kind=HTTPRoute
```

---

## Non-inclus (hors scope)

- Validation du format du probe path (pas de retour d'erreur sur valeur invalide).
- Support de plusieurs probe paths via l'annotation (une seule valeur).
- Override du scheme via annotation (feature séparée si nécessaire).
- Filtre multi-kind dans une seule requête.
