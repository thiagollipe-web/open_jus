// Package llm reúne os clientes de IA do OpenJus: a IA gratuita via API
// compatível com OpenAI (/chat/completions — ex.: Groq), o SLM local
// (Ollama) e o cliente Claude (Anthropic Messages API, com tool use).
// Tradução direta de src/openjus/ia_livre.py, slm.py e bot.py (cliente).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
)

// SistemaIA é o prompt de sistema da IA gratuita (ia_livre.SISTEMA).
const SistemaIA = "Você é o OpenJus, assistente jurídico informativo do Brasil. Regras OBRIGATÓRIAS:\n" +
	"1. Responda SÓ o que foi perguntado, em no máximo 4 frases curtas, em português simples.\n" +
	"2. Formato: uma frase de resposta direta; depois, se útil, até 2 tópicos com '•' do que fazer.\n" +
	"3. Sem introdução, sem repetir a pergunta, sem conclusão, sem 'espero ter ajudado'.\n" +
	"4. Se houver CONTEXTO, use SOMENTE ele. Sem contexto, NÃO cite números de artigos, leis ou " +
	"prazos; dê orientação geral e diga para confirmar com advogado ou Defensoria Pública.\n" +
	"5. Se a pergunta não for sobre Direito brasileiro, diga em 1 frase que só ajuda com temas " +
	"jurídicos.\n" +
	"6. Nunca invente fatos. Se não souber, diga: 'Não sei responder com segurança.'"

// SistemaSLM é o prompt de sistema do SLM local (slm.SISTEMA).
const SistemaSLM = "Você é o OpenJus, assistente jurídico informativo do Brasil. Responda em português " +
	"simples e curto usando SOMENTE o CONTEXTO. Se o contexto não responder, diga exatamente: " +
	"\"Não sei responder com segurança.\" Não invente leis, artigos nem prazos."

// FreeLLM é o cliente da IA gratuita (endpoint OpenAI-compatível).
type FreeLLM struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// Configurado diz se a IA gratuita está configurada (URL + modelo).
func (c *FreeLLM) Configurado() bool {
	return c != nil && c.BaseURL != "" && c.Model != ""
}

// Perguntar pede um resumo ancorado nos trechos (ou orientação geral sem
// trechos). Devolve "" quando a IA não está configurada ou falha — o bot
// segue por regras, como no Python.
func (c *FreeLLM) Perguntar(ctx context.Context, pergunta string, trechos []string) string {
	if !c.Configurado() {
		return ""
	}
	user := ""
	if len(trechos) > 0 {
		user = "CONTEXTO:\n- " + strings.Join(trechos, "\n- ") + "\n\n"
	}
	user += "PERGUNTA: " + pergunta
	body, _ := json.Marshal(map[string]interface{}{
		"model": c.Model, "temperature": 0, "max_tokens": 280,
		"messages": []map[string]string{
			{"role": "system", "content": SistemaIA},
			{"role": "user", "content": user},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Warn("ia_livre_falhou", "erro", err.Error())
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		slog.Warn("ia_livre_falhou", "status", resp.StatusCode, "corpo", string(corpo))
		return ""
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Choices) == 0 {
		slog.Warn("ia_livre_falhou", "erro", "resposta inválida")
		return ""
	}
	txt := Enxugar(strings.TrimSpace(out.Choices[0].Message.Content))
	if txt == "" {
		return ""
	}
	return txt
}

// Enxugar remove clichês e corta respostas longas na última frase completa.
func Enxugar(txt string, maxChars ...int) string {
	max := 900
	if len(maxChars) > 0 {
		max = maxChars[0]
	}
	txt = strings.TrimSpace(lingua.RemoverCliches(txt))
	r := []rune(txt)
	if len(r) <= max {
		return txt
	}
	// Último ". " ou "\n" dentro dos primeiros max caracteres.
	corte := -1
	for i := max - 2; i >= 0; i-- {
		if r[i] == '.' && r[i+1] == ' ' {
			corte = i
			break
		}
	}
	for i := max - 1; i >= 0; i-- {
		if r[i] == '\n' && i > corte {
			corte = i
			break
		}
	}
	if corte > 0 {
		return strings.TrimSpace(string(r[:corte+1]))
	}
	return strings.TrimSpace(string(r[:max]))
}

// SLM é o cliente do modelo local (Ollama).
type SLM struct {
	URL    string
	Model  string
	HTTP   *http.Client
}

// Resumir resume SOMENTE a partir dos trechos. Devolve "" sem Ollama,
// sem trechos, sem resposta ou quando o modelo diz que não sabe.
func (s *SLM) Resumir(ctx context.Context, pergunta string, trechos []string) string {
	if s == nil || s.URL == "" || len(trechos) == 0 {
		return ""
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model": s.Model, "stream": false, "options": map[string]interface{}{"temperature": 0},
		"messages": []map[string]string{
			{"role": "system", "content": SistemaSLM},
			{"role": "user", "content": "CONTEXTO:\n- " + strings.Join(trechos, "\n- ") + "\n\nPERGUNTA: " + pergunta},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.URL, "/")+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	httpClient := s.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ""
	}
	txt := strings.TrimSpace(out.Message.Content)
	if txt == "" || strings.Contains(strings.ToLower(txt), "não sei responder") {
		return ""
	}
	return txt
}

// --- Cliente Claude (Anthropic Messages API) ---

// Mensagem é uma mensagem da conversa (role + content em texto ou blocos).
type Mensagem struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string ou []Bloco
}

// Bloco é um bloco de conteúdo da resposta do Claude.
type Bloco struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
}

// Ferramenta é uma tool definition do Claude.
type Ferramenta struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ClaudeClient chama a API de mensagens da Anthropic.
type ClaudeClient struct {
	APIKey  string
	Model   string
	HTTP    *http.Client
	BaseURL string // padrão: https://api.anthropic.com (testes usam um servidor local)
}

// baseURL devolve a base da API.
func (c *ClaudeClient) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return "https://api.anthropic.com"
}

// RespostaClaude é uma resposta da API de mensagens.
type RespostaClaude struct {
	StopReason string  `json:"stop_reason"`
	Content    []Bloco `json:"content"`
	Tokens     int
}

// CriarMensagem faz uma chamada à API de mensagens.
func (c *ClaudeClient) CriarMensagem(ctx context.Context, sistema string, ferramentas []Ferramenta, mensagens []Mensagem, maxTokens int) (*RespostaClaude, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": c.Model, "max_tokens": maxTokens, "system": sistema,
		"tools": ferramentas, "messages": mensagens,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL()+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return nil, fmt.Errorf("Anthropic respondeu HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(corpo)))
	}
	var out struct {
		StopReason string  `json:"stop_reason"`
		Content    []Bloco `json:"content"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &RespostaClaude{
		StopReason: out.StopReason,
		Content:    out.Content,
		Tokens:     out.Usage.InputTokens + out.Usage.OutputTokens,
	}, nil
}

// SistemaClaude monta o prompt de sistema do bot com LLM (data de hoje).
func SistemaClaude(hoje time.Time) string {
	return fmt.Sprintf(`Você é o OpenJus, assistente jurídico informativo para o Direito brasileiro. Hoje é %s.
Regras:
- Responda em português do Brasil, em linguagem simples, curta e direta.
- Prazos: SEMPRE use a ferramenta calcular_prazo. Nunca calcule de cabeça.
- Leis: SEMPRE use buscar_legislacao antes de citar texto legal; cite título e URN retornados. Se nada for encontrado, diga que não encontrou. Não invente artigos.
- Processos: use consultar_processo quando houver número CNJ. Explique as movimentações em linguagem simples.
- Você não substitui advogado; indique isso quando a pergunta pedir aconselhamento sobre caso concreto.
- Processos em segredo de justiça: não tente contornar.`, hoje.Format("02/01/2006"))
}

// FerramentasClaude são as 4 ferramentas do bot com LLM (bot.TOOLS).
var FerramentasClaude = []Ferramenta{
	{
		Name: "calcular_prazo",
		Description: "Calcula prazo processual pelo CPC (arts. 219, 220, 224). Datas AAAA-MM-DD.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"data_inicio":   map[string]interface{}{"type": "string", "description": "Data da intimação/publicação"},
				"dias":          map[string]interface{}{"type": "integer"},
				"dias_uteis":    map[string]interface{}{"type": "boolean", "default": true},
				"feriados_extras": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"data_inicio", "dias"},
		},
	},
	{
		Name: "buscar_legislacao",
		Description: "Busca trechos na legislação indexada (ctresb/leis), com URN e situação.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"consulta": map[string]interface{}{"type": "string"},
				"limite":   map[string]interface{}{"type": "integer", "default": 5},
			},
			"required": []string{"consulta"},
		},
	},
	{
		Name: "consultar_processo",
		Description: "Consulta processo no DataJud pelo número CNJ (NNNNNNN-DD.AAAA.J.TT.OOOO).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"numero_cnj": map[string]interface{}{"type": "string"},
			},
			"required": []string{"numero_cnj"},
		},
	},
	{
		Name: "explicar_movimento",
		Description: "Traduz o nome de uma movimentação processual para linguagem simples.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"nome_movimento": map[string]interface{}{"type": "string"},
			},
			"required": []string{"nome_movimento"},
		},
	},
}
