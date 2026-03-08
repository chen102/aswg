package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/adapter/codex"
	"agent-session-web-gateway/backend/internal/server"
)

func main() {
	cfg := server.LoadConfig()
	cfg.FrontendDir = server.ResolveFrontendDir(cfg.FrontendDir)

	registry := adapter.NewRegistry(cfg.DefaultAdapter)
	enabled := make(map[string]struct{}, len(cfg.EnabledAdapters))
	for _, name := range cfg.EnabledAdapters {
		enabled[name] = struct{}{}
	}

	if _, ok := enabled["codex"]; ok {
		codexAdapter, err := codex.NewAdapter(cfg.SeedFile)
		if err != nil {
			log.Fatalf("init codex adapter failed: %v", err)
		}
		registry.Register(codexAdapter)
	}

	if len(registry.List()) == 0 {
		log.Fatal("no adapters enabled")
	}

	srv := server.New(cfg, registry)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("server exited with error: %v", err)
	}
}
