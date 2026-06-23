// Command k8s-http-discovery runs an HTTP server that exposes Kubernetes
// service endpoints in the Prometheus HTTP SD format.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/Ronan-WeScale/k8s_http_discovery/internal/collector"
	"github.com/Ronan-WeScale/k8s_http_discovery/internal/config"
	"github.com/Ronan-WeScale/k8s_http_discovery/internal/server"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("build in-cluster config: %v", err)
	}

	k8sClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("create kubernetes client: %v", err)
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("create dynamic client: %v", err)
	}

	var collectors []collector.Collector
	for _, name := range cfg.Collectors {
		switch name {
		case "ingress":
			collectors = append(collectors, collector.NewIngressCollector(k8sClient, cfg))
		case "httproute":
			collectors = append(collectors, collector.NewHTTPRouteCollector(dynClient, cfg))
		case "apisixroute":
			collectors = append(collectors, collector.NewApisixRouteCollector(dynClient, cfg))
		default:
			log.Printf("unknown collector %q — skipping", name)
		}
	}

	mgr := server.NewManager(collectors, cfg.CacheTTL)
	mgr.Start(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/targets", mgr.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux}

	// Serve in a goroutine so we can listen for context cancellation.
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
