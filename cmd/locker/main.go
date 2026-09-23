package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/Hanibal-AI/locker/internal/config"
	"github.com/Hanibal-AI/locker/internal/pii"
	"github.com/Hanibal-AI/locker/internal/providers"
	"github.com/Hanibal-AI/locker/internal/proxy"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	provider, err := providers.New(cfg.Provider, cfg.ActiveProvider())
	if err != nil {
		log.Fatalf("provider error: %v", err)
	}

	piiEngine, err := pii.NewEngine(cfg.PII)
	if err != nil {
		log.Fatalf("pii config error: %v", err)
	}

	server := proxy.New(proxy.Options{
		Provider:            provider,
		RequestTimeout:      cfg.RequestTimeout,
		AllowedModels:       cfg.AllowedModels,
		PII:                 piiEngine,
		StreamLookbackBytes: cfg.PII.StreamLookbackBytes,
		StreamIdleTimeout:   cfg.StreamIdleTimeout,
	})

	log.Printf("locker listening on %s (provider=%s, pii_masking=%t)", cfg.ListenAddr, cfg.Provider, !cfg.PII.Disabled)
	if err := http.ListenAndServe(cfg.ListenAddr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}
