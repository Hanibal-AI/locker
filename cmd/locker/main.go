package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/Hanibal-AI/locker/internal/config"
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

	server := proxy.New(provider, cfg.RequestTimeout, cfg.AllowedModels)

	log.Printf("locker listening on %s (provider=%s)", cfg.ListenAddr, cfg.Provider)
	if err := http.ListenAndServe(cfg.ListenAddr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}
