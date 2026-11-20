// Command api sobe o servidor HTTP do OpenJus (backend em Go).
// Lê a configuração das variáveis de ambiente (e de .env), na mesma
// forma que o antigo backend Python.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/thiagollipe-web/open_jus/api-go/internal/api"
	"github.com/thiagollipe-web/open_jus/api-go/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuração inválida", "erro", err)
		os.Exit(1)
	}
	nivel := slog.LevelInfo
	switch cfg.LogLevel {
	case "DEBUG":
		nivel = slog.LevelDebug
	case "WARN", "WARNING":
		nivel = slog.LevelWarn
	case "ERROR":
		nivel = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: nivel})))

	srv, err := api.NewServer(cfg)
	if err != nil {
		slog.Error("não foi possível montar o servidor", "erro", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Servir(ctx); err != nil {
		slog.Error("servidor parou", "erro", err)
		os.Exit(1)
	}
}
