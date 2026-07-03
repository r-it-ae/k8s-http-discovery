package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Ensure no env vars are set
	os.Unsetenv("PORT")
	os.Unsetenv("NAMESPACES")
	os.Unsetenv("COLLECTORS")
	os.Unsetenv("DEFAULT_SCHEME")
	os.Unsetenv("CACHE_TTL")
	os.Unsetenv("REQUIRE_ANNOTATION")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.RequireAnnotation {
		t.Errorf("expected RequireAnnotation=false by default, got true")
	}
	if cfg.Port != "8080" {
		t.Errorf("expected Port=8080, got %q", cfg.Port)
	}
	if len(cfg.Namespaces) != 0 {
		t.Errorf("expected empty Namespaces, got %v", cfg.Namespaces)
	}
	if len(cfg.Collectors) != 3 {
		t.Errorf("expected 3 default collectors, got %v", cfg.Collectors)
	}
	if cfg.Collectors[0] != "ingress" || cfg.Collectors[1] != "httproute" || cfg.Collectors[2] != "apisixroute" {
		t.Errorf("unexpected default collectors: %v", cfg.Collectors)
	}
	if cfg.DefaultScheme != "https" {
		t.Errorf("expected DefaultScheme=https, got %q", cfg.DefaultScheme)
	}
	if cfg.CacheTTL != 30*time.Second {
		t.Errorf("expected CacheTTL=30s, got %v", cfg.CacheTTL)
	}
}

func TestLoadNamespacesCSV(t *testing.T) {
	os.Setenv("NAMESPACES", "ns1,ns2")
	defer os.Unsetenv("NAMESPACES")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(cfg.Namespaces) != 2 {
		t.Fatalf("expected 2 namespaces, got %v", cfg.Namespaces)
	}
	if cfg.Namespaces[0] != "ns1" || cfg.Namespaces[1] != "ns2" {
		t.Errorf("unexpected namespaces: %v", cfg.Namespaces)
	}
}

func TestLoadCacheTTL(t *testing.T) {
	os.Setenv("CACHE_TTL", "1m")
	defer os.Unsetenv("CACHE_TTL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.CacheTTL != time.Minute {
		t.Errorf("expected CacheTTL=1m, got %v", cfg.CacheTTL)
	}
}

func TestLoadInvalidCacheTTL(t *testing.T) {
	os.Setenv("CACHE_TTL", "notaduration")
	defer os.Unsetenv("CACHE_TTL")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid CACHE_TTL, got nil")
	}
}

func TestLoadRequireAnnotation(t *testing.T) {
	os.Setenv("REQUIRE_ANNOTATION", "true")
	defer os.Unsetenv("REQUIRE_ANNOTATION")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !cfg.RequireAnnotation {
		t.Errorf("expected RequireAnnotation=true, got false")
	}
}

func TestLoadInvalidRequireAnnotation(t *testing.T) {
	os.Setenv("REQUIRE_ANNOTATION", "notabool")
	defer os.Unsetenv("REQUIRE_ANNOTATION")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid REQUIRE_ANNOTATION, got nil")
	}
}
