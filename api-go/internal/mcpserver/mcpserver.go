// Package mcpserver expõe as ferramentas do OpenJus via MCP (stdio) —
// Claude Desktop, Cursor e outros agentes.
// Tradução direta de src/openjus/mcp_server.py (FastMCP), sobre os
// pacotes Go já portados (servico, leis, lingua, referencias).
//
// As 5 tools, nomes, parâmetros e formatos de resultado são os mesmos do
// Python; a única diferença é o transporte interno: o SDK oficial Go para
// MCP (github.com/modelcontextprotocol/go-sdk) em vez do FastMCP.
package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
	"github.com/thiagollipe-web/open_jus/api-go/internal/referencias"
	"github.com/thiagollipe-web/open_jus/api-go/internal/servico"
)

// Versao é a versão do servidor (a mesma da API HTTP).
const Versao = "0.1.0"

// Server agrega as dependências das tools.
type Server struct {
	Servico    *servico.Servico
	Lingua     *lingua.Lingua
	LeisDBPath string
}

// New monta um *mcp.Server com as 5 tools do OpenJus.
func New(s *Server) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "openjus", Version: Versao}, nil)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "consultar_processo",
		Description: "Consulta um processo no DataJud pelo número CNJ (NNNNNNN-DD.AAAA.J.TT.OOOO).",
	}, s.consultarProcesso)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "calcular_prazo",
		Description: "Calcula prazo processual (CPC arts. 219, 220, 224). Datas em AAAA-MM-DD.",
	}, s.calcularPrazo)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "buscar_legislacao",
		Description: "Busca trechos na legislação indexada (ctresb/leis), com URN e situação.",
	}, s.buscarLegislacao)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "explicar_movimento",
		Description: "Traduz o nome de uma movimentação processual para linguagem simples.",
	}, s.explicarMovimento)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "qual_conector_usar",
		Description: "Indica o repositório do ecossistema ideal (lote, dataframe, agente, tjsp, scraping).",
	}, s.qualConectorUsar)
	return srv
}

// ConsultarProcessoInput são os argumentos da tool consultar_processo.
type ConsultarProcessoInput struct {
	NumeroCNJ string `json:"numero_cnj" jsonschema:"Número CNJ do processo (NNNNNNN-DD.AAAA.J.TT.OOOO)."`
}

func (s *Server) consultarProcesso(ctx context.Context, _ *mcp.CallToolRequest, in ConsultarProcessoInput) (*mcp.CallToolResult, *servico.ResultadoProcesso, error) {
	r, err := s.Servico.ConsultarProcesso(ctx, in.NumeroCNJ)
	if err != nil {
		return nil, nil, err
	}
	return nil, r, nil
}

// CalcularPrazoInput são os argumentos da tool calcular_prazo.
type CalcularPrazoInput struct {
	DataInicio     string   `json:"data_inicio" jsonschema:"Data de início (AAAA-MM-DD)."`
	Dias           int      `json:"dias" jsonschema:"Número de dias do prazo."`
	DiasUteis      *bool    `json:"dias_uteis,omitempty" jsonschema:"Contar apenas dias úteis (padrão: true)."`
	FeriadosExtras []string `json:"feriados_extras,omitempty" jsonschema:"Feriados extras em AAAA-MM-DD."`
}

func (s *Server) calcularPrazo(_ context.Context, _ *mcp.CallToolRequest, in CalcularPrazoInput) (*mcp.CallToolResult, servico.ResultadoPrazo, error) {
	inicio, err := time.Parse("2006-01-02", in.DataInicio)
	if err != nil {
		return nil, servico.ResultadoPrazo{}, fmt.Errorf("data_inicio inválida (use AAAA-MM-DD): %s", in.DataInicio)
	}
	diasUteis := true
	if in.DiasUteis != nil {
		diasUteis = *in.DiasUteis
	}
	var extras []time.Time
	for _, d := range in.FeriadosExtras {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			return nil, servico.ResultadoPrazo{}, fmt.Errorf("feriado extra inválido (use AAAA-MM-DD): %s", d)
		}
		extras = append(extras, t)
	}
	r, err := servico.Prazo(inicio, in.Dias, extras, diasUteis)
	if err != nil {
		return nil, servico.ResultadoPrazo{}, err
	}
	return nil, r, nil
}

// BuscarLegislacaoInput são os argumentos da tool buscar_legislacao.
type BuscarLegislacaoInput struct {
	Consulta       string `json:"consulta" jsonschema:"Consulta em linguagem natural."`
	Limite         *int   `json:"limite,omitempty" jsonschema:"Número máximo de trechos (padrão: 5)."`
	ApenasVigentes *bool  `json:"apenas_vigentes,omitempty" jsonschema:"Buscar só leis vigentes (padrão: true)."`
}

// TrechoResultado é um trecho de lei com score — os mesmos campos do
// t.__dict__ do Python (o JSON de /api/leis omite o score; aqui, como no
// Python, ele vai no resultado da tool).
type TrechoResultado struct {
	Texto       string  `json:"texto"`
	Path        string  `json:"path"`
	Titulo      string  `json:"titulo"`
	Urn         string  `json:"urn"`
	Situacao    string  `json:"situacao"`
	Fonte       *string `json:"fonte"`
	VersaoTexto *string `json:"versao_texto"`
	BaixadoEm   *string `json:"baixado_em"`
	Ementa      string  `json:"ementa"`
	Score       float64 `json:"score"`
}

func (s *Server) buscarLegislacao(_ context.Context, _ *mcp.CallToolRequest, in BuscarLegislacaoInput) (*mcp.CallToolResult, []TrechoResultado, error) {
	limite := 5
	if in.Limite != nil {
		limite = *in.Limite
	}
	apenasVigentes := true
	if in.ApenasVigentes != nil {
		apenasVigentes = *in.ApenasVigentes
	}
	trechos, err := leis.Buscar(s.LeisDBPath, in.Consulta, limite, apenasVigentes)
	if err != nil {
		return nil, nil, err
	}
	out := make([]TrechoResultado, 0, len(trechos))
	for _, t := range trechos {
		out = append(out, TrechoResultado{
			Texto: t.Texto, Path: t.Path, Titulo: t.Titulo, Urn: t.Urn,
			Situacao: t.Situacao, Fonte: t.Fonte, VersaoTexto: t.VersaoTexto,
			BaixadoEm: t.BaixadoEm, Ementa: t.Ementa, Score: t.Score(),
		})
	}
	return nil, out, nil
}

// ExplicarMovimentoInput são os argumentos da tool explicar_movimento.
type ExplicarMovimentoInput struct {
	NomeMovimento string `json:"nome_movimento" jsonschema:"Nome da movimentação processual."`
}

// O Python devolve a explicação como texto puro (não JSON); aqui o
// conteúdo é o texto puro e o structuredContent fica vazio, igual.
func (s *Server) explicarMovimento(_ context.Context, _ *mcp.CallToolRequest, in ExplicarMovimentoInput) (*mcp.CallToolResult, any, error) {
	texto := s.Lingua.TraduzirMovimento(in.NomeMovimento)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: texto}},
	}, nil, nil
}

// QualConectorUsarInput são os argumentos da tool qual_conector_usar.
type QualConectorUsarInput struct {
	CasoDeUso string `json:"caso_de_uso" jsonschema:"Caso de uso: lote, dataframe, agente, tjsp, scraping ou legislacao (ou a chave exata)."`
}

func (s *Server) qualConectorUsar(_ context.Context, _ *mcp.CallToolRequest, in QualConectorUsarInput) (*mcp.CallToolResult, referencias.Conector, error) {
	c, ok := referencias.Recomendar(in.CasoDeUso)
	if !ok {
		chaves := make([]string, 0, len(referencias.Conectores))
		for k := range referencias.Conectores {
			chaves = append(chaves, k)
		}
		sort.Strings(chaves)
		return nil, referencias.Conector{}, fmt.Errorf("Caso desconhecido: %s. Opções: %v", in.CasoDeUso, chaves)
	}
	return nil, c, nil
}
