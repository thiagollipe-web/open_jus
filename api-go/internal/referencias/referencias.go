// Package referencias é a camada 3 (Referências): qual conector do
// ecossistema GitHub usar em cada caso de uso.
// Tradução direta de src/openjus/referencias.py.
package referencias

// Conector é um projeto do ecossistema recomendado para um caso de uso.
type Conector struct {
	Repo       string `json:"repo"`
	Linguagem  string `json:"linguagem"`
	QuandoUsar string `json:"quando_usar"`
}

// Conectores é o catálogo completo (mesma ordem do Python).
var Conectores = map[string]Conector{
	"lote_csv":           {Repo: "DanielFillol/DataJUD_API_CALLER", Linguagem: "Go", QuandoUsar: "Extração em massa assíncrona a partir de CSV de números CNJ."},
	"analise_dataframe":  {Repo: "jespimentel/api_cnj", Linguagem: "Python/Pandas", QuandoUsar: "Análise estatística e DataFrames de movimentações."},
	"agente_mcp":         {Repo: "DeHor-Labs/mcp-juridico-brasil", Linguagem: "Python/MCP", QuandoUsar: "Automação nativa por agente de IA via servidor MCP."},
	"tjsp_esaj":          {Repo: "jjesusfilho/tjsp", Linguagem: "R", QuandoUsar: "TJSP/e-SAJ e API mobile quando a API REST pública falha."},
	"crawler_generico":   {Repo: "thiagozs/crawler-jus", Linguagem: "NodeJS/Python", QuandoUsar: "Raspagem local de portais de tribunais sem API."},
	"legislacao":         {Repo: "ctresb/leis", Linguagem: "Markdown", QuandoUsar: "Acervo de legislação (CF, leis federais/estaduais) com YAML."},
}

// Ordem é a ordem de inserção do catálogo (para respostas determinísticas).
var Ordem = []string{"lote_csv", "analise_dataframe", "agente_mcp", "tjsp_esaj", "crawler_generico", "legislacao"}

// Recomendar devolve o conector de um caso (atalho ou chave exata).
func Recomendar(caso string) (Conector, bool) {
	atalhos := map[string]string{
		"lote": "lote_csv", "dataframe": "analise_dataframe", "agente": "agente_mcp",
		"tjsp": "tjsp_esaj", "esaj": "tjsp_esaj", "scraping": "crawler_generico",
		"legislacao": "legislacao",
	}
	if chave, ok := atalhos[caso]; ok {
		caso = chave
	}
	c, ok := Conectores[caso]
	return c, ok
}
