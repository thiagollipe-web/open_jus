// Package servico orquestra as 4 camadas — usado pela API e (no Python)
// pelo servidor MCP. Tradução direta de src/openjus/servico.py.
package servico

import (
	"context"
	"time"

	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
	"github.com/thiagollipe-web/open_jus/api-go/internal/memoria"
	"github.com/thiagollipe-web/open_jus/api-go/internal/prazos"
)

// Servico agrega as dependências da orquestração.
type Servico struct {
	LeisDBPath  string // índice de leis
	MutableDB   string // estado mutável (memória)
	DataJud     *datajud.Client
	Lingua      *lingua.Lingua
}

// MovimentoExplicado é um movimento com a explicação em linguagem simples.
type MovimentoExplicado struct {
	Data       string `json:"data,omitempty"`
	Nome       string `json:"nome"`
	Explicacao string `json:"explicacao"`
}

// LegislacaoRelacionada é um trecho de lei relacionado ao processo.
type LegislacaoRelacionada struct {
	Titulo  string `json:"titulo"`
	Urn     string `json:"urn"`
	Arquivo string `json:"arquivo"`
	Trecho  string `json:"trecho"`
}

// ResultadoProcesso é o retorno de ConsultarProcesso.
type ResultadoProcesso struct {
	Encontrado               bool                    `json:"encontrado"`
	Sigiloso                 bool                    `json:"sigiloso,omitempty"`
	Aviso                    string                  `json:"aviso,omitempty"`
	Classe                   string                  `json:"classe,omitempty"`
	Assuntos                 []string                `json:"assuntos,omitempty"`
	OrgaoJulgador            string                  `json:"orgao_julgador,omitempty"`
	Movimentos               []MovimentoExplicado    `json:"movimentos,omitempty"`
	NovosDesdeUltimaConsulta []string                `json:"novos_desde_ultima_consulta,omitempty"`
	LegislacaoRelacionada    []LegislacaoRelacionada `json:"legislacao_relacionada,omitempty"`
}

// ConsultarProcesso consulta o DataJud, registra o snapshot (LGPD) e cruza
// classe/assuntos com a legislação vigente.
func (s *Servico) ConsultarProcesso(ctx context.Context, numero string) (*ResultadoProcesso, error) {
	p, err := s.DataJud.Consultar(ctx, numero)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return &ResultadoProcesso{Encontrado: false}, nil
	}
	novos, err := memoria.NovosMovimentos(s.MutableDB, p)
	if err != nil {
		novos = []string{}
	}
	if _, err := memoria.SalvarSnapshot(s.MutableDB, p); err != nil {
		// Snapshot é best-effort: a consulta não falha por causa dele.
		_ = err
	}
	if p.NivelSigilo > 0 {
		return &ResultadoProcesso{
			Encontrado: true, Sigiloso: true,
			Aviso: "Processo em segredo de justiça: dados não exibidos nem armazenados.",
		}, nil
	}
	// Divergência documentada do Python: o Python cruzava com o banco
	// mutável (sempre vazio); o Go cruza com o índice real de leis.
	var legislacao []LegislacaoRelacionada
	if s.LeisDBPath != "" && leis.IndiceDisponivel(s.LeisDBPath) {
		trechos, err := leis.CruzarProcesso(s.LeisDBPath, p.Classe, p.Assuntos, 3)
		if err == nil {
			for _, t := range trechos {
				legislacao = append(legislacao, LegislacaoRelacionada{
					Titulo: t.Titulo, Urn: t.Urn, Arquivo: t.Path, Trecho: fatiarRunas(t.Texto, 500),
				})
			}
		}
	}
	movs := p.Movimentos
	if len(movs) > 20 {
		movs = movs[:20]
	}
	explicados := make([]MovimentoExplicado, 0, len(movs))
	for _, m := range movs {
		explicados = append(explicados, MovimentoExplicado{
			Data: m.Data, Nome: m.Nome, Explicacao: s.Lingua.TraduzirMovimento(m.Nome),
		})
	}
	if novos == nil {
		novos = []string{}
	}
	if legislacao == nil {
		legislacao = []LegislacaoRelacionada{}
	}
	return &ResultadoProcesso{
		Encontrado: true, Sigiloso: false, Classe: p.Classe, Assuntos: p.Assuntos,
		OrgaoJulgador: p.OrgaoJulgador, Movimentos: explicados,
		NovosDesdeUltimaConsulta: novos, LegislacaoRelacionada: legislacao,
	}, nil
}

func fatiarRunas(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// ResultadoPrazo é o cálculo de um prazo processual.
type ResultadoPrazo struct {
	InicioEvento   string   `json:"inicio_evento"`
	InicioContagem string   `json:"inicio_contagem"`
	Vencimento     string   `json:"vencimento"`
	Dias           int      `json:"dias"`
	Fundamentos    []string `json:"fundamentos"`
	Aviso          string   `json:"aviso"`
}

// Prazo calcula o termo final de um prazo a partir da data de início.
func Prazo(inicio time.Time, dias int, feriadosExtras []time.Time, diasUteis bool) (ResultadoPrazo, error) {
	extras := map[time.Time]bool{}
	for _, d := range feriadosExtras {
		extras[d] = true
	}
	r, err := prazos.CalcularPrazo(inicio, dias, prazos.Calendario{FeriadosExtras: extras}, diasUteis)
	if err != nil {
		return ResultadoPrazo{}, err
	}
	return ResultadoPrazo{
		InicioEvento:   r.InicioEvento.Format("2006-01-02"),
		InicioContagem: r.InicioContagem.Format("2006-01-02"),
		Vencimento:     r.Vencimento.Format("2006-01-02"),
		Dias:           r.Dias,
		Fundamentos:    r.Fundamentos,
		Aviso:          "Confira feriados locais e a data efetiva da intimação.",
	}, nil
}

// PrazoMapa é a mesma resposta em map (para os tool results do Claude).
func PrazoMapa(inicio time.Time, dias int, feriadosExtras []time.Time, diasUteis bool) (map[string]interface{}, error) {
	r, err := Prazo(inicio, dias, feriadosExtras, diasUteis)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"inicio_evento":   r.InicioEvento,
		"inicio_contagem": r.InicioContagem,
		"vencimento":      r.Vencimento,
		"dias":            r.Dias,
		"fundamentos":     r.Fundamentos,
		"aviso":           r.Aviso,
	}, nil
}
