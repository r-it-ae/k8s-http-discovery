# Design : annotation probe-path

**Date:** 2026-06-23
**Scope:** Ajout d'une annotation Kubernetes permettant de surcharger le path de monitoring sur les ressources Ingress, HTTPRoute et ApisixRoute.

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

## Non-inclus (hors scope)

- Validation du format du path (pas de retour d'erreur sur valeur invalide).
- Support de plusieurs probe paths via l'annotation (une seule valeur).
- Override du scheme via annotation (feature séparée si nécessaire).
