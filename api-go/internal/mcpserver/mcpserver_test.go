package mcpserver_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
	"github.com/thiagollipe-web/open_jus/api-go/internal/mcpserver"
	"github.com/thiagollipe-web/open_jus/api-go/internal/servico"
)

// leiFixture é um acervo mínimo (1 lei) para o índice de teste.
const leiFixture = `---
titulo: "Lei nº 8.078 de 11/09/1990"
urn: "urn:lex:br:federal:lei:1990-09-11;8078"
situacao: "vigente"
ementa: "Dispõe sobre a proteção do consumidor e dá outras providências."
fonte: "https://normas.leg.br/?urn=urn:lex:br:federal:lei:1990-09-11;8078"
baixado_em: 2026-10-13
---
# Lei nº 8.078 de 11/09/1990

Art. 1º O presente código estabelece normas de proteção e defesa do consumidor.

Art. 2º Consumidor é toda pessoa física ou jurídica que adquire ou utiliza produto ou serviço.

Art. 3º O prazo de contestação é de 15 dias, contados da citação do fornecedor.
`

// montaSessao sobe o servidor MCP em memória e devolve a sessão do cliente.
// O índice de leis é construído a partir da fixture; o DataJud fica sem
// chave (o caminho de erro é testado, não a rede).
func montaSessao(t *testing.T) (*mcp.ClientSession, context.CancelFunc) {
	t.Helper()
	dir := t.TempDir()
	acervo := filepath.Join(dir, "acervo")
	if err := os.MkdirAll(acervo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acervo, "lei-8078.md"), []byte(leiFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	leisDB := filepath.Join(dir, "leis.db")
	mutavelDB := filepath.Join(dir, "mutavel.db")
	if _, err := leis.Ingerir(acervo, leisDB, mutavelDB); err != nil {
		t.Fatalf("Ingerir: %v", err)
	}
	ling, err := lingua.CarregarGlossario("")
	if err != nil {
		t.Fatalf("CarregarGlossario: %v", err)
	}
	sv := &servico.Servico{
		LeisDBPath: leisDB,
		MutableDB:  mutavelDB,
		DataJud:    datajud.NewClient("", ""),
		Lingua:     ling,
	}
	srv := mcpserver.New(&mcpserver.Server{Servico: sv, Lingua: ling, LeisDBPath: leisDB})

	ctx, cancel := context.WithCancel(context.Background())
	ct, st := mcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, st) }()
	cli := mcp.NewClient(&mcp.Implementation{Name: "teste", Version: "0.0.0"}, nil)
	cs, err := cli.Connect(ctx, ct, nil)
	if err != nil {
		cancel()
		t.Fatalf("Connect: %v", err)
	}
	return cs, cancel
}

func chama(t *testing.T, cs *mcp.ClientSession, nome string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: nome, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", nome, err)
	}
	return res
}

func textoConteudo(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func estruturado(t *testing.T, res *mcp.CallToolResult, alvo any) {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, alvo); err != nil {
		t.Fatalf("unmarshal StructuredContent (%s): %v", string(raw), err)
	}
}

func TestListToolsTemAs5Tools(t *testing.T) {
	cs, cancel := montaSessao(t)
	defer cancel()
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	nomes := map[string]bool{}
	for _, tool := range res.Tools {
		nomes[tool.Name] = true
	}
	esperados := []string{"consultar_processo", "calcular_prazo", "buscar_legislacao", "explicar_movimento", "qual_conector_usar"}
	if len(res.Tools) != len(esperados) {
		t.Fatalf("esperava %d tools, vieram %d", len(esperados), len(res.Tools))
	}
	for _, n := range esperados {
		if !nomes[n] {
			t.Errorf("falta a tool %q", n)
		}
	}
}

func TestListToolsObrigatorios(t *testing.T) {
	cs, cancel := montaSessao(t)
	defer cancel()
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	// obrigatórios do Python (os com valor padrão não podem ser obrigatórios)
	esperado := map[string][]string{
		"consultar_processo": {"numero_cnj"},
		"calcular_prazo":     {"data_inicio", "dias"},
		"buscar_legislacao":  {"consulta"},
		"explicar_movimento": {"nome_movimento"},
		"qual_conector_usar": {"caso_de_uso"},
	}
	for _, tool := range res.Tools {
		q, ok := esperado[tool.Name]
		if !ok {
			continue
		}
		// No lado do cliente o schema vem como mapa genérico.
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("%s: marshal InputSchema: %v", tool.Name, err)
		}
		var schema struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: unmarshal InputSchema: %v", tool.Name, err)
		}
		req := map[string]bool{}
		for _, r := range schema.Required {
			req[r] = true
		}
		if len(req) != len(q) {
			t.Errorf("%s: obrigatórios %v, esperava %v", tool.Name, schema.Required, q)
		}
		for _, r := range q {
			if !req[r] {
				t.Errorf("%s: %q deveria ser obrigatório", tool.Name, r)
			}
		}
	}
}

func TestCalcularPrazo(t *testing.T) {
	cs, cancel := montaSessao(t)
	defer cancel()
	res := chama(t, cs, "calcular_prazo", map[string]any{"data_inicio": "2025-03-10", "dias": 5})
	if res.IsError {
		t.Fatalf("erro inesperado: %s", textoConteudo(t, res))
	}
	var out struct {
		Vencimento string `json:"vencimento"`
	}
	estruturado(t, res, &out)
	if out.Vencimento != "2025-03-17" {
		t.Errorf("vencimento = %q, esperava 2025-03-17", out.Vencimento)
	}
	// data inválida vira erro da tool (como a exceção no Python)
	res = chama(t, cs, "calcular_prazo", map[string]any{"data_inicio": "10/03/2025", "dias": 5})
	if !res.IsError {
		t.Errorf("data inválida deveria dar erro, veio: %s", textoConteudo(t, res))
	}
}

func TestExplicarMovimento(t *testing.T) {
	cs, cancel := montaSessao(t)
	defer cancel()
	res := chama(t, cs, "explicar_movimento", map[string]any{"nome_movimento": "trânsito em julgado"})
	if res.IsError {
		t.Fatalf("erro inesperado: %s", textoConteudo(t, res))
	}
	// O Python devolve a explicação como texto puro — o conteúdo aqui
	// também é texto puro, sem aspas de JSON.
	if got := textoConteudo(t, res); got != "A decisão não admite mais recurso; tornou-se definitiva." {
		t.Errorf("explicação = %q", got)
	}
}

func TestQualConectorUsar(t *testing.T) {
	cs, cancel := montaSessao(t)
	defer cancel()
	res := chama(t, cs, "qual_conector_usar", map[string]any{"caso_de_uso": "lote"})
	if res.IsError {
		t.Fatalf("erro inesperado: %s", textoConteudo(t, res))
	}
	var out struct {
		Repo string `json:"repo"`
	}
	estruturado(t, res, &out)
	if out.Repo != "DanielFillol/DataJUD_API_CALLER" {
		t.Errorf("repo = %q", out.Repo)
	}
	// Caso desconhecido: o Python levanta KeyError; aqui vira erro da tool.
	res = chama(t, cs, "qual_conector_usar", map[string]any{"caso_de_uso": "xyz"})
	if !res.IsError {
		t.Errorf("caso desconhecido deveria dar erro")
	}
	if got := textoConteudo(t, res); !strings.Contains(got, "Caso desconhecido") {
		t.Errorf("erro = %q, esperava mencionar 'Caso desconhecido'", got)
	}
}

func TestBuscarLegislacao(t *testing.T) {
	cs, cancel := montaSessao(t)
	defer cancel()
	res := chama(t, cs, "buscar_legislacao", map[string]any{"consulta": "consumidor"})
	if res.IsError {
		t.Fatalf("erro inesperado: %s", textoConteudo(t, res))
	}
	var out []mcpserver.TrechoResultado
	estruturado(t, res, &out)
	if len(out) == 0 {
		t.Fatal("esperava ao menos 1 trecho")
	}
	if !strings.Contains(out[0].Titulo, "8.078") {
		t.Errorf("primeiro título = %q", out[0].Titulo)
	}
	// Como no t.__dict__ do Python, o score vai no resultado da tool.
	if out[0].Score == 0 {
		t.Errorf("score ausente no resultado da tool")
	}
}

func TestConsultarProcessoErros(t *testing.T) {
	cs, cancel := montaSessao(t)
	defer cancel()
	// DV inválido — igual ao Python.
	res := chama(t, cs, "consultar_processo", map[string]any{"numero_cnj": "0801234-56.2024.8.26.0100"})
	if !res.IsError {
		t.Errorf("DV inválido deveria dar erro")
	}
	if got := textoConteudo(t, res); !strings.Contains(got, "Dígito verificador") {
		t.Errorf("erro = %q", got)
	}
	// CNJ válido sem chave DataJud — caminho de erro claro.
	res = chama(t, cs, "consultar_processo", map[string]any{"numero_cnj": "0000001-78.2020.8.26.0100"})
	if !res.IsError {
		t.Errorf("sem chave DataJud deveria dar erro")
	}
	if got := textoConteudo(t, res); !strings.Contains(got, "DATAJUD_API_KEY") {
		t.Errorf("erro = %q", got)
	}
}
