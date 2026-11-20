package bot_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/aprender"
	"github.com/thiagollipe-web/open_jus/api-go/internal/bot"
	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
	"github.com/thiagollipe-web/open_jus/api-go/internal/llm"
	"github.com/thiagollipe-web/open_jus/api-go/internal/servico"
)

func novoBotLocal(t *testing.T, leisDB, mutavel string) *bot.BotLocal {
	t.Helper()
	ling, err := lingua.CarregarGlossario("")
	if err != nil {
		t.Fatalf("glossário: %v", err)
	}
	sv := &servico.Servico{
		LeisDBPath: leisDB, MutableDB: mutavel,
		DataJud: datajud.NewClient("https://api-publica.datajud.cnj.jus.br", ""),
		Lingua:  ling,
	}
	return &bot.BotLocal{LeisDBPath: leisDB, MutableDB: mutavel, Lingua: ling, Servico: sv}
}

func TestBotLocalPrazoTermoEAjuda(t *testing.T) {
	b := novoBotLocal(t, filepath.Join(t.TempDir(), "nada.db"), filepath.Join(t.TempDir(), "x.db"))
	ctx := context.Background()

	r := b.Conversar(ctx, "Fui intimado em 07/03/2025 e tenho prazo de 15 dias úteis")
	if !strings.Contains(r.Resposta, "28/03/2025") {
		t.Fatalf("prazo = %q, quero 28/03/2025", r.Resposta)
	}
	if len(r.Ferramentas) != 1 || r.Ferramentas[0] != "calcular_prazo" {
		t.Fatalf("ferramentas = %v, quero [calcular_prazo]", r.Ferramentas)
	}

	r = b.Conversar(ctx, "o que significa trânsito em julgado?")
	if !strings.Contains(r.Resposta, "definitiva") {
		t.Fatalf("termo = %q, quero 'definitiva'", r.Resposta)
	}

	r = b.Conversar(ctx, "prazo de 10 dias") // falta a data
	if !strings.Contains(strings.ToLower(r.Resposta), "data") {
		t.Fatalf("prazo sem data = %q, quero menção a 'data'", r.Resposta)
	}

	r = b.Conversar(ctx, "0000001-00.2020.8.26.0100") // DV inválido
	if !strings.Contains(r.Resposta, "inválido") {
		t.Fatalf("CNJ inválido = %q, quero 'inválido'", r.Resposta)
	}

	r = b.Conversar(ctx, "")
	if !strings.HasPrefix(r.Resposta, "Posso ajudar") {
		t.Fatalf("vazio = %q, quero começar com 'Posso ajudar'", r.Resposta)
	}
}

func TestBotPedeCNJEcumprimenta(t *testing.T) {
	b := novoBotLocal(t, filepath.Join(t.TempDir(), "nada.db"), filepath.Join(t.TempDir(), "x.db"))
	ctx := context.Background()

	r := b.Conversar(ctx, "Quero saber de um processo")
	if !strings.Contains(r.Resposta, "número CNJ") {
		t.Fatalf("processo sem CNJ = %q, quero 'número CNJ'", r.Resposta)
	}
	r = b.Conversar(ctx, "Olá")
	if !strings.HasPrefix(r.Resposta, "Olá!") {
		t.Fatalf("saudação = %q, quero começar com 'Olá!'", r.Resposta)
	}
	r = b.Conversar(ctx, "posso ser despejado?") // sem índice e sem IA: nada técnico
	if strings.Contains(r.Resposta, "openjus-ingest") {
		t.Fatalf("resposta menciona openjus-ingest: %q", r.Resposta)
	}
}

func TestBotTermoDesconhecidoNaoCaiNoFallbackDeGlossario(t *testing.T) {
	b := novoBotLocal(t, filepath.Join(t.TempDir(), "nada.db"), filepath.Join(t.TempDir(), "x.db"))
	ctx := context.Background()

	// Termo fora do glossário: a pergunta segue para a busca de leis, não
	// para o "Andamento processual (sem tradução cadastrada)".
	r := b.Conversar(ctx, "o que significa usucapião extraordinária?")
	if strings.Contains(r.Resposta, "Andamento processual") {
		t.Fatalf("caiu no fallback de glossário: %q", r.Resposta)
	}
	if !strings.Contains(r.Resposta, "Ainda não sei responder") {
		t.Fatalf("resposta = %q, quero 'Ainda não sei responder'", r.Resposta)
	}
}

func TestBotClaudeUsaFerramentaDePrazo(t *testing.T) {
	var chamadas int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		n := atomic.AddInt32(&chamadas, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = w.Write([]byte(`{"stop_reason": "tool_use", "usage": {"input_tokens": 10, "output_tokens": 5}, "content": [{"type": "tool_use", "id": "t1", "name": "calcular_prazo", "input": {"data_inicio": "2025-03-07", "dias": 15}}]}`))
			return
		}
		// O modelo recebeu o resultado determinístico.
		var req struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(corpo, &req)
		ultimo := ""
		if len(req.Messages) > 0 {
			ultimo = string(req.Messages[len(req.Messages)-1].Content)
		}
		if !strings.Contains(ultimo, "2025-03-28") {
			t.Errorf("tool result sem 2025-03-28: %s", ultimo)
		}
		_, _ = w.Write([]byte(`{"stop_reason": "end_turn", "usage": {"input_tokens": 20, "output_tokens": 5}, "content": [{"type": "text", "text": "Vence em 28/03/2025."}]}`))
	}))
	defer srv.Close()

	ling, err := lingua.CarregarGlossario("")
	if err != nil {
		t.Fatal(err)
	}
	sv := &servico.Servico{
		LeisDBPath: filepath.Join(t.TempDir(), "nada.db"),
		MutableDB:  filepath.Join(t.TempDir(), "x.db"),
		DataJud:    datajud.NewClient("https://api-publica.datajud.cnj.jus.br", ""),
		Lingua:     ling,
	}
	b := &bot.BotClaude{
		Cliente: &llm.ClaudeClient{APIKey: "k", Model: "claude-sonnet-5-5", BaseURL: srv.URL},
		Servico: sv, Lingua: ling,
	}
	out, err := b.Conversar(context.Background(), []llm.Mensagem{{Role: "user", Content: "prazo?"}})
	if err != nil {
		t.Fatalf("Conversar: %v", err)
	}
	if len(out.Ferramentas) != 1 || out.Ferramentas[0] != "calcular_prazo" {
		t.Fatalf("ferramentas = %v, quero [calcular_prazo]", out.Ferramentas)
	}
	if !strings.Contains(out.Resposta, "28/03") {
		t.Fatalf("resposta = %q, quero 28/03", out.Resposta)
	}
}

func TestAprendizadoRecorrente(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "leis")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "x.md"),
		[]byte("---\ntitulo: X\nsituacao: vigente\n---\nArt. 1 Nada relevante."), 0o644); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "l.db")
	if _, err := leis.Ingerir(d, db, filepath.Join(dir, "mutavel.db")); err != nil {
		t.Fatalf("Ingerir: %v", err)
	}
	b := novoBotLocal(t, db, db)
	ctx := context.Background()

	p := "Qual o horário de atendimento do fórum trabalhista?"
	r := b.Conversar(ctx, p)
	if !strings.Contains(r.Resposta, "ensinar:") { // não sabe -> lacuna
		t.Fatalf("resposta = %q, quero 'ensinar:'", r.Resposta)
	}
	lac, err := aprender.Lacunas(db, 10)
	if err != nil || len(lac) != 1 || lac[0].N != 1 {
		t.Fatalf("lacunas = %+v, %v", lac, err)
	}
	if _, err := aprender.Ensinar(db, p, "Consulte o site do TRT."); err != nil {
		t.Fatalf("Ensinar: %v", err)
	}
	r = b.Conversar(ctx, p)
	if !strings.Contains(r.Resposta, "ensinar:") { // pendente não vale
		t.Fatalf("resposta pendente = %q, quero 'ensinar:'", r.Resposta)
	}
	pend, err := aprender.Pendentes(db)
	if err != nil || len(pend) != 1 {
		t.Fatalf("pendentes = %+v, %v", pend, err)
	}
	ok, err := aprender.Aprovar(db, pend[0].ID)
	if err != nil || !ok {
		t.Fatalf("Aprovar: %v, %v", ok, err)
	}
	r = b.Conversar(ctx, "horário de atendimento do fórum trabalhista")
	if !strings.Contains(r.Resposta, "TRT") {
		t.Fatalf("resposta = %q, quero 'TRT'", r.Resposta)
	}
	if len(r.Ferramentas) != 1 || r.Ferramentas[0] != "aprendizado" {
		t.Fatalf("ferramentas = %v, quero [aprendizado]", r.Ferramentas)
	}
}
