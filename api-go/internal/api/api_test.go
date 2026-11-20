package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/api"
	"github.com/thiagollipe-web/open_jus/api-go/internal/bot"
	"github.com/thiagollipe-web/open_jus/api-go/internal/config"
	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
	"github.com/thiagollipe-web/open_jus/api-go/internal/servico"
)

func cfgBase(t *testing.T) (config.Settings, string) {
	t.Helper()
	dir := t.TempDir()
	return config.Settings{
		DBPath:          filepath.Join(dir, "openjus.db"),
		LeisDBPath:      filepath.Join(dir, "leis.db"),
		AdminToken:      "adm",
		FreeDailyTokens: 5000,
		RateLimitPerMin: 1000,
		DataJudBaseURL:  "https://api-publica.datajud.cnj.jus.br",
		CORSOrigins:     "https://thiagollipe-web.github.io",
		SiteDir:         filepath.Join(dir, "site-ausente"),
		GlossarioPath:   "",
		Port:            "8000",
	}, dir
}

func requisicao(t *testing.T, srv *api.Server, metodo, caminho string, corpo interface{}, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var leitor *bytes.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			t.Fatal(err)
		}
		leitor = bytes.NewReader(bruto)
	} else {
		leitor = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(metodo, caminho, leitor)
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAPIStatusSemSegredos(t *testing.T) {
	cfg, _ := cfgBase(t)
	cfg.FreeLLMAPIKey = "segredo"
	cfg.FreeLLMModel = "modelo-secreto"
	cfg.FreeLLMBaseURL = "https://exemplo.test/v1"
	cfg.DataJudAPIKey = "chave-secreta"
	srv, err := api.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := requisicao(t, srv, "GET", "/api/status", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d, corpo %s", rec.Code, rec.Body.String())
	}
	corpo := rec.Body.String()
	if !strings.Contains(corpo, `"status":"ok"`) || !strings.Contains(corpo, "indice_de_leis") {
		t.Fatalf("corpo = %s", corpo)
	}
	for _, proibido := range []string{"segredo", "modelo-secreto", "chave-secreta", "exemplo.test",
		"ia_gratuita_configurada", "ia_modelo", "ia_url", "ia_chave_definida", "datajud_chave_definida"} {
		if strings.Contains(corpo, proibido) {
			t.Fatalf("status expõe %q: %s", proibido, corpo)
		}
	}
}

func TestAPIChatLLM402E429(t *testing.T) {
	cfg, _ := cfgBase(t)
	cfg.FreeDailyTokens = 0
	srv, err := api.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	corpo := map[string]interface{}{"mensagens": []map[string]string{{"role": "user", "content": "oi"}}}

	rec := requisicao(t, srv, "POST", "/api/chat-llm", corpo, nil)
	if rec.Code != 401 {
		t.Fatalf("sem chave = %d, quero 401", rec.Code)
	}
	rec = requisicao(t, srv, "POST", "/admin/chaves", map[string]interface{}{}, nil)
	if rec.Code != 403 {
		t.Fatalf("chaves sem token = %d, quero 403", rec.Code)
	}
	rec = requisicao(t, srv, "POST", "/admin/chaves", map[string]interface{}{},
		map[string]string{"X-Admin-Token": "adm"})
	if rec.Code != 200 {
		t.Fatalf("chaves com token = %d, corpo %s", rec.Code, rec.Body.String())
	}
	var chaveResp struct {
		Chave string `json:"chave"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &chaveResp); err != nil || chaveResp.Chave == "" {
		t.Fatalf("chave inválida: %s", rec.Body.String())
	}
	rec = requisicao(t, srv, "POST", "/api/chat-llm", corpo,
		map[string]string{"X-OpenJus-Key": chaveResp.Chave})
	if rec.Code != 402 { // sem franquia e sem créditos
		t.Fatalf("sem saldo = %d, quero 402", rec.Code)
	}

	// Limite de taxa: 2 por minuto.
	cfg2, _ := cfgBase(t)
	cfg2.RateLimitPerMin = 2
	srv2, err := api.NewServer(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	var codigos []int
	for i := 0; i < 3; i++ {
		codigos = append(codigos, requisicao(t, srv2, "GET", "/api/saude", nil, nil).Code)
	}
	if codigos[0] != 200 || codigos[1] != 200 || codigos[2] != 429 {
		t.Fatalf("códigos = %v, quero [200 200 429]", codigos)
	}
}

func TestAPIChatGratisSemChave(t *testing.T) {
	cfg, _ := cfgBase(t)
	srv, err := api.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := requisicao(t, srv, "POST", "/api/chat",
		map[string]interface{}{"mensagens": []map[string]string{
			{"role": "user", "content": "prazo de 5 dias a partir de 10/03/2025"}}}, nil)
	if rec.Code != 200 {
		t.Fatalf("chat = %d, corpo %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "17/03/2025") {
		t.Fatalf("resposta = %s, quero 17/03/2025", rec.Body.String())
	}
}

func TestAPILeisRealEIndiceAusente(t *testing.T) {
	corpus := filepath.Join("..", "..", "..", "data", "leis_iniciais")
	if _, err := os.Stat(corpus); err != nil {
		t.Skip("corpus real ausente")
	}
	cfg, dir := cfgBase(t)
	srv, err := api.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := requisicao(t, srv, "GET", "/api/leis?q=consumidor", nil, nil)
	if rec.Code != 503 {
		t.Fatalf("índice ausente = %d, quero 503", rec.Code)
	}
	if _, err := os.Stat(cfg.LeisDBPath); err == nil {
		t.Fatal("a consulta 503 não devia criar o índice")
	}
	if _, err := leis.Ingerir(corpus, cfg.LeisDBPath, filepath.Join(dir, "mutavel.db")); err != nil {
		t.Fatalf("Ingerir: %v", err)
	}
	rec = requisicao(t, srv, "GET", "/api/leis?q=consumidor+fornecedor&limite=3", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("leis = %d, corpo %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), ";8078") {
		t.Fatalf("resposta sem CDC (;8078): %s", rec.Body.String())
	}
	rec = requisicao(t, srv, "GET", "/api/status", nil, nil)
	if !strings.Contains(rec.Body.String(), `"indice_de_leis":true`) {
		t.Fatalf("status = %s, quero indice_de_leis true", rec.Body.String())
	}
}

func TestRaizRedirecionaSemSite(t *testing.T) {
	cfg, _ := cfgBase(t)
	srv, err := api.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := requisicao(t, srv, "GET", "/", nil, nil)
	if rec.Code != 307 {
		t.Fatalf("raiz = %d, quero 307", rec.Code)
	}
	if !strings.HasSuffix(rec.Header().Get("Location"), "/open_jus/") {
		t.Fatalf("location = %q, quero terminar em /open_jus/", rec.Header().Get("Location"))
	}
}

func TestAPIAprenderViaHTTP(t *testing.T) {
	cfg, dir := cfgBase(t)
	// Índice sintético no mesmo arquivo do banco mutável (como no teste Python).
	d := filepath.Join(dir, "leis")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "x.md"),
		[]byte("---\ntitulo: X\nsituacao: vigente\n---\nArt. 1 Nada relevante."), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := leis.Ingerir(d, cfg.DBPath, filepath.Join(dir, "mutavel.db")); err != nil {
		t.Fatalf("Ingerir: %v", err)
	}
	srv, err := api.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	admin := map[string]string{"X-Admin-Token": "adm"}
	pergunta := "Qual o horário de atendimento do fórum trabalhista?"

	rec := requisicao(t, srv, "POST", "/api/ensinar",
		map[string]interface{}{"pergunta": pergunta, "resposta": "Consulte o site do TRT."}, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "pendente") {
		t.Fatalf("ensinar = %d %s", rec.Code, rec.Body.String())
	}
	rec = requisicao(t, srv, "GET", "/admin/pendentes", nil, admin)
	if rec.Code != 200 {
		t.Fatalf("pendentes = %d", rec.Code)
	}
	var pend struct {
		Pendentes []struct {
			ID int64 `json:"id"`
		} `json:"pendentes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pend); err != nil || len(pend.Pendentes) != 1 {
		t.Fatalf("pendentes = %s", rec.Body.String())
	}
	rec = requisicao(t, srv, "POST", "/admin/aprovar",
		map[string]interface{}{"id": pend.Pendentes[0].ID}, admin)
	if rec.Code != 200 {
		t.Fatalf("aprovar = %d %s", rec.Code, rec.Body.String())
	}

	// O bot (mesmo banco) agora responde o que aprendeu.
	ling, err := lingua.CarregarGlossario("")
	if err != nil {
		t.Fatal(err)
	}
	sv := &servico.Servico{
		LeisDBPath: cfg.DBPath, MutableDB: cfg.DBPath,
		DataJud: datajud.NewClient("https://api-publica.datajud.cnj.jus.br", ""),
		Lingua:  ling,
	}
	b := &bot.BotLocal{LeisDBPath: cfg.DBPath, MutableDB: cfg.DBPath, Lingua: ling, Servico: sv}
	r := b.Conversar(t.Context(), "horário de atendimento do fórum trabalhista")
	if !strings.Contains(r.Resposta, "TRT") {
		t.Fatalf("resposta = %q, quero 'TRT'", r.Resposta)
	}
	if len(r.Ferramentas) != 1 || r.Ferramentas[0] != "aprendizado" {
		t.Fatalf("ferramentas = %v, quero [aprendizado]", r.Ferramentas)
	}
}

func TestAPILeisSemQ(t *testing.T) {
	cfg, _ := cfgBase(t)
	srv, err := api.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := requisicao(t, srv, "GET", "/api/leis", nil, nil)
	if rec.Code != 422 {
		t.Fatalf("leis sem q = %d, quero 422", rec.Code)
	}
}
