package llm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/llm"
)

func TestIALivreOpenAICompat(t *testing.T) {
	visto := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visto["url"] = r.URL.Path
		visto["auth"] = r.Header.Get("Authorization")
		corpo, _ := io.ReadAll(r.Body)
		visto["body"] = string(corpo)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "Explicação simples."}}]}`))
	}))
	defer srv.Close()

	c := &llm.FreeLLM{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "modelo-gratis"}
	got := c.Perguntar(context.Background(), "o que é usucapião?", []string{"trecho X"})
	if got != "Explicação simples." {
		t.Fatalf("Perguntar = %q", got)
	}
	if visto["url"] != "/v1/chat/completions" {
		t.Fatalf("url = %q, quero /v1/chat/completions", visto["url"])
	}
	if visto["auth"] != "Bearer k" {
		t.Fatalf("auth = %q, quero Bearer k", visto["auth"])
	}
	if !strings.Contains(visto["body"], "trecho X") || !strings.Contains(visto["body"], "modelo-gratis") {
		t.Fatalf("body sem trecho/modelo: %s", visto["body"])
	}

	// Limite grátis (429) -> segue por regras (retorno vazio).
	srv429 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv429.Close()
	c429 := &llm.FreeLLM{BaseURL: srv429.URL, APIKey: "k", Model: "m"}
	if got := c429.Perguntar(context.Background(), "p", nil); got != "" {
		t.Fatalf("429 devia dar vazio, deu %q", got)
	}
	// Sem configuração -> vazio.
	if got := (&llm.FreeLLM{}).Perguntar(context.Background(), "p", nil); got != "" {
		t.Fatalf("sem config devia dar vazio, deu %q", got)
	}
}

func TestSLMOllamaAncorado(t *testing.T) {
	visto := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		visto["body"] = string(corpo)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message": {"content": "Resumo simples."}}`))
	}))
	defer srv.Close()

	s := &llm.SLM{URL: srv.URL, Model: "qwen2.5:0.5b"}
	got := s.Resumir(context.Background(), "pergunta", []string{"trecho A"})
	if got != "Resumo simples." {
		t.Fatalf("Resumir = %q", got)
	}
	if !strings.Contains(visto["body"], "trecho A") || !strings.Contains(visto["body"], "SOMENTE o CONTEXTO") {
		t.Fatalf("body sem trecho/contexto: %s", visto["body"])
	}
	// Sem contexto não chama o modelo.
	if got := s.Resumir(context.Background(), "pergunta", nil); got != "" {
		t.Fatalf("sem trechos devia dar vazio, deu %q", got)
	}
	// Modelo diz que não sabe -> vazio.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message": {"content": "Não sei responder com segurança."}}`))
	}))
	defer srv2.Close()
	s2 := &llm.SLM{URL: srv2.URL, Model: "qwen2.5:0.5b"}
	if got := s2.Resumir(context.Background(), "p", []string{"x"}); got != "" {
		t.Fatalf("não-sei devia dar vazio, deu %q", got)
	}
	// Sem URL -> vazio.
	if got := (&llm.SLM{}).Resumir(context.Background(), "p", []string{"x"}); got != "" {
		t.Fatalf("sem URL devia dar vazio, deu %q", got)
	}
}

func TestIAEnxugaResposta(t *testing.T) {
	longo := "Ótima pergunta! " + strings.Repeat("Frase longa sobre o tema. ", 60) + "Espero ter ajudado."
	out := llm.Enxugar(longo)
	if len([]rune(out)) > 900 {
		t.Fatalf("len = %d, quero <= 900", len([]rune(out)))
	}
	if !strings.HasSuffix(out, ".") {
		t.Fatalf("não termina com ponto: %q", out[len(out)-20:])
	}
	if strings.Contains(out, "Ótima pergunta") {
		t.Fatalf("clichê não removido: %q", out[:50])
	}
	if !strings.Contains(llm.SistemaIA, "4 frases") || !strings.Contains(llm.SistemaIA, "só ajuda com temas") {
		t.Fatal("SistemaIA sem as regras esperadas")
	}
}

func TestFerramentasClaude(t *testing.T) {
	// As 4 ferramentas do bot com LLM, com os mesmos nomes do Python.
	nomes := map[string]bool{}
	for _, f := range llm.FerramentasClaude {
		nomes[f.Name] = true
	}
	for _, quero := range []string{"calcular_prazo", "buscar_legislacao", "consultar_processo", "explicar_movimento"} {
		if !nomes[quero] {
			t.Fatalf("falta a ferramenta %s", quero)
		}
	}
}
