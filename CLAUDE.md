# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Prometheus HTTP SD server for Kubernetes — discovers HTTP targets from Ingress, HTTPRoute (Gateway API), and ApisixRoute CRs and exposes them in [Prometheus HTTP SD format](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_sd_config).

## Commands

```bash
go build ./...          # compile
go test ./...           # run all tests
go test ./internal/...  # run unit tests only
go run ./cmd/           # run locally (needs KUBECONFIG or in-cluster)
```

## Architecture

- `internal/config/` — env-var config loading (`PORT`, `NAMESPACES`, `COLLECTORS`, `DEFAULT_SCHEME`, `CACHE_TTL`, `REQUIRE_ANNOTATION`)
- `internal/collector/` — `Collector` interface + three implementations (Ingress, HTTPRoute, ApisixRoute)
- `internal/server/` — HTTP handler + background cache refresh goroutine
- `cmd/main.go` — wire-up: in-cluster config, collector selection, server start
- `deploy/` — Kubernetes manifests (RBAC, Deployment, Service)

## Key decisions

- ApisixRoute uses the dynamic client (no official Go types); HTTPRoute uses the dynamic client too for consistency
- Cache is refreshed every `CACHE_TTL` (default 30s) by a background goroutine; handler always reads from cache
- Each host×path pair from a CR becomes one SD target entry
- `NAMESPACES=""` means all namespaces (cluster-wide list)
