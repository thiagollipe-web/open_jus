// Package config lê a configuração do backend a partir de variáveis de
// ambiente (e de um arquivo .env, se existir). Os nomes e os padrões são
// os mesmos do antigo backend Python (src/openjus/config.py); nenhum
// segredo é registrado em log.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Settings reúne toda a configuração do servidor.
type Settings struct {
	AnthropicAPIKey  string
	AnthropicModel   string
	FreeLLMBaseURL   string
	FreeLLMAPIKey    string
	FreeLLMModel     string
	CORSOrigins      string
	OllamaURL        string
	OllamaModel      string
	AdminToken       string
	FreeDailyTokens  int
	RateLimitPerMin  int
	DataJudAPIKey    string
	DataJudBaseURL   string
	LeisDir          string
	DBPath           string // estado mutável: memória, aprendizado, créditos
	LeisDBPath       string // índice somente de legislação (nunca = DBPath)
	LogLevel         string
	Port             string
	SiteDir          string
	GlossarioPath    string
}

// Defaults espelham config.py.
func defaults() Settings {
	return Settings{
		AnthropicModel:  "claude-sonnet-5-5",
		CORSOrigins:     "https://thiagollipe-web.github.io",
		OllamaModel:     "qwen2.5:0.5b",
		FreeDailyTokens: 5000,
		RateLimitPerMin: 60,
		DataJudBaseURL:  "https://api-publica.datajud.cnj.jus.br",
		LeisDir:         "./data/leis",
		DBPath:          "./data/openjus.db",
		LeisDBPath:      "./data/leis.db",
		LogLevel:        "INFO",
		Port:            "8000",
		SiteDir:         "site",
		GlossarioPath:   "site/glossario.json",
	}
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v // variável presente (mesmo vazia) vence o padrão, como no Python
	}
	return def
}

func getenvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// loadDotEnv carrega KEY=VALUE de um arquivo .env (linhas vazias e
// comentários # são ignorados). Variáveis já presentes no ambiente vencem.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		} else if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = v[1 : len(v)-1]
		}
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
}

func sameFile(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil && ra == rb {
		return true
	}
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ia, ib)
	}
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return aa == bb
}

// Load lê a configuração (carrega .env antes, como o Python fazia).
func Load() (Settings, error) {
	loadDotEnv(".env")
	d := defaults()
	s := Settings{
		AnthropicAPIKey: getenv("ANTHROPIC_API_KEY", d.AnthropicAPIKey),
		AnthropicModel:  getenv("ANTHROPIC_MODEL", d.AnthropicModel),
		FreeLLMBaseURL:  getenv("FREE_LLM_BASE_URL", d.FreeLLMBaseURL),
		FreeLLMAPIKey:   getenv("FREE_LLM_API_KEY", d.FreeLLMAPIKey),
		FreeLLMModel:    getenv("FREE_LLM_MODEL", d.FreeLLMModel),
		CORSOrigins:     getenv("CORS_ORIGINS", d.CORSOrigins),
		OllamaURL:       getenv("OLLAMA_URL", d.OllamaURL),
		OllamaModel:     getenv("OLLAMA_MODEL", d.OllamaModel),
		AdminToken:      getenv("ADMIN_TOKEN", d.AdminToken),
		FreeDailyTokens: getenvInt("FREE_DAILY_TOKENS", d.FreeDailyTokens),
		RateLimitPerMin: getenvInt("RATE_LIMIT_PER_MIN", d.RateLimitPerMin),
		DataJudAPIKey:   getenv("DATAJUD_API_KEY", d.DataJudAPIKey),
		DataJudBaseURL:  getenv("DATAJUD_BASE_URL", d.DataJudBaseURL),
		LeisDir:         getenv("LEIS_DIR", d.LeisDir),
		DBPath:          getenv("DB_PATH", d.DBPath),
		LeisDBPath:      getenv("LEIS_DB_PATH", d.LeisDBPath),
		LogLevel:        getenv("LOG_LEVEL", d.LogLevel),
		Port:            getenv("PORT", d.Port),
		SiteDir:         getenv("SITE_DIR", d.SiteDir),
		GlossarioPath:   getenv("GLOSSARIO_PATH", d.GlossarioPath),
	}
	if sameFile(s.DBPath, s.LeisDBPath) {
		return s, fmt.Errorf("LEIS_DB_PATH e DB_PATH não podem apontar para o mesmo arquivo")
	}
	return s, nil
}

// CORSAllowedOrigins devolve a lista de origens permitidas.
func (s Settings) CORSAllowedOrigins() []string {
	var out []string
	for _, o := range strings.Split(s.CORSOrigins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}
