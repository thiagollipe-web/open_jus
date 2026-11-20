package config_test

import (
	"strings"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/config"
)

func TestLoadPadroes(t *testing.T) {
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CORS_ORIGINS", "")
	t.Setenv("DB_PATH", "./data/openjus.db")
	t.Setenv("LEIS_DB_PATH", "./data/leis.db")
	t.Setenv("FREE_DAILY_TOKENS", "")
	t.Setenv("RATE_LIMIT_PER_MIN", "")
	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Variável presente (mesmo vazia) vence o padrão, como no Python.
	if s.AnthropicModel != "" {
		t.Fatalf("ANTHROPIC_MODEL vazio devia ficar vazio, deu %q", s.AnthropicModel)
	}
	if s.FreeDailyTokens != 5000 || s.RateLimitPerMin != 60 {
		t.Fatalf("padrões: franquia=%d limite=%d", s.FreeDailyTokens, s.RateLimitPerMin)
	}
	if s.DataJudBaseURL != "https://api-publica.datajud.cnj.jus.br" {
		t.Fatalf("base DataJud = %q", s.DataJudBaseURL)
	}
}

func TestLoadRecusaMesmoBanco(t *testing.T) {
	t.Setenv("DB_PATH", "x.db")
	t.Setenv("LEIS_DB_PATH", "x.db")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "não podem apontar") {
		t.Fatalf("erro = %v, quero 'não podem apontar'", err)
	}
}

func TestCORSAllowedOrigins(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "https://a.test, https://b.test ,,")
	s, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	origens := s.CORSAllowedOrigins()
	if len(origens) != 2 || origens[0] != "https://a.test" || origens[1] != "https://b.test" {
		t.Fatalf("origens = %v", origens)
	}
}
