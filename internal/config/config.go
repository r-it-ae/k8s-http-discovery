package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port              string        // PORT, default "8080"
	Namespaces        []string      // NAMESPACES CSV, default [] (all)
	Collectors        []string      // COLLECTORS CSV, default ["ingress","httproute","apisixroute"]
	DefaultScheme     string        // DEFAULT_SCHEME, default "https"
	CacheTTL          time.Duration // CACHE_TTL, default 30s
	RequireAnnotation bool          // REQUIRE_ANNOTATION, default false (discover everything)
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:          "8080",
		Namespaces:    []string{},
		Collectors:    []string{"ingress", "httproute", "apisixroute"},
		DefaultScheme: "https",
		CacheTTL:      30 * time.Second,
	}

	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}

	if v := os.Getenv("NAMESPACES"); v != "" {
		cfg.Namespaces = splitCSV(v)
	}

	if v := os.Getenv("COLLECTORS"); v != "" {
		cfg.Collectors = splitCSV(v)
	}

	if v := os.Getenv("DEFAULT_SCHEME"); v != "" {
		cfg.DefaultScheme = v
	}

	if v := os.Getenv("CACHE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CACHE_TTL %q: %w", v, err)
		}
		cfg.CacheTTL = d
	}

	if v := os.Getenv("REQUIRE_ANNOTATION"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid REQUIRE_ANNOTATION %q: %w", v, err)
		}
		cfg.RequireAnnotation = b
	}

	return cfg, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
