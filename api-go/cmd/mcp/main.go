// Command mcp sobe o servidor MCP (stdio) do OpenJus — expõe as
// ferramentas a agentes (Claude Desktop, Cursor) na mesma forma que o
// antigo servidor Python (src/openjus/mcp_server.py).
//
// Uso: openjus-mcp (lê a configuração das variáveis de ambiente e de .env,
// como o servidor HTTP). Logs vão para stderr; o stdout é só o protocolo.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/thiagollipe-web/open_jus/api-go/internal/config"
	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
	"github.com/thiagollipe-web/open_jus/api-go/internal/mcpserver"
	"github.com/thiagollipe-web/open_jus/api-go/internal/servico"
)

// carregarGlossario lê site/glossario.json quando existe; senão usa a
// cópia embutida no binário. Espelha internal/api.carregarGlossario.
func carregarGlossario(path string) (*lingua.Lingua, error) {
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return lingua.CarregarGlossario(path)
		}
	}
	return lingua.CarregarGlossario("")
}

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

	ling, err := carregarGlossario(cfg.GlossarioPath)
	if err != nil {
		slog.Error("glossário", "erro", err)
		os.Exit(1)
	}
	sv := &servico.Servico{
		LeisDBPath: cfg.LeisDBPath,
		MutableDB:  cfg.DBPath,
		DataJud:    datajud.NewClient(cfg.DataJudBaseURL, cfg.DataJudAPIKey),
		Lingua:     ling,
	}
	srv := mcpserver.New(&mcpserver.Server{Servico: sv, Lingua: ling, LeisDBPath: cfg.LeisDBPath})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		slog.Error("servidor MCP parou", "erro", err)
		os.Exit(1)
	}
}
