// Package datajud é o cliente assíncrono da API Pública do DataJud (CNJ).
// Endpoint: POST {base}/api_publica_<alias>/_search, header Authorization: APIKey <chave>.
package datajud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/thiagollipe-web/open_jus/api-go/internal/cnj"
)

// DataJudError é um erro da API pública do DataJud (ou da falta de chave).
type DataJudError struct{ msg string }

func (e *DataJudError) Error() string { return e.msg }

// Movimento é uma movimentação processual.
type Movimento struct {
	Codigo *int   `json:"codigo,omitempty"`
	Nome   string `json:"nome"`
	Data   string `json:"data,omitempty"`
}

// ProcessoResumo é o resumo de um processo no DataJud.
type ProcessoResumo struct {
	Numero         string      `json:"numero"`
	Tribunal       string      `json:"tribunal,omitempty"`
	Classe         string      `json:"classe,omitempty"`
	Assuntos       []string    `json:"assuntos"`
	OrgaoJulgador  string      `json:"orgao_julgador,omitempty"`
	Grau           string      `json:"grau,omitempty"`
	DataAjuizamento string     `json:"data_ajuizamento,omitempty"`
	NivelSigilo    int         `json:"nivel_sigilo"`
	Movimentos     []Movimento `json:"movimentos"`
}

type datajudHit struct {
	Source struct {
		NumeroProcesso string `json:"numeroProcesso"`
		Tribunal       string `json:"tribunal"`
		Classe         struct {
			Nome string `json:"nome"`
		} `json:"classe"`
		Assuntos []struct {
			Nome string `json:"nome"`
		} `json:"assuntos"`
		OrgaoJulgador struct {
			Nome string `json:"nome"`
		} `json:"orgaoJulgador"`
		Grau            string `json:"grau"`
		DataAjuizamento string `json:"dataAjuizamento"`
		NivelSigilo     int    `json:"nivelSigilo"`
		Movimentos      []struct {
			Codigo   *int   `json:"codigo"`
			Nome     string `json:"nome"`
			DataHora string `json:"dataHora"`
		} `json:"movimentos"`
	} `json:"_source"`
}

// normalizar converte um hit do DataJud em ProcessoResumo, com as
// movimentações ordenadas da mais recente para a mais antiga.
func normalizar(hit datajudHit) ProcessoResumo {
	s := hit.Source
	movs := make([]Movimento, 0, len(s.Movimentos))
	for _, m := range s.Movimentos {
		movs = append(movs, Movimento{Codigo: m.Codigo, Nome: m.Nome, Data: m.DataHora})
	}
	sort.SliceStable(movs, func(i, j int) bool { return movs[i].Data > movs[j].Data })
	assuntos := make([]string, 0, len(s.Assuntos))
	for _, a := range s.Assuntos {
		assuntos = append(assuntos, a.Nome)
	}
	return ProcessoResumo{
		Numero: s.NumeroProcesso, Tribunal: s.Tribunal, Classe: s.Classe.Nome,
		Assuntos: assuntos, OrgaoJulgador: s.OrgaoJulgador.Nome, Grau: s.Grau,
		DataAjuizamento: s.DataAjuizamento, NivelSigilo: s.NivelSigilo, Movimentos: movs,
	}
}

// Client consulta a API Pública do DataJud.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient cria um cliente com timeout de 30s (igual ao Python).
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Consultar busca um processo pelo número CNJ. Devolve nil se não encontrado.
func (c *Client) Consultar(ctx context.Context, numero string) (*ProcessoResumo, error) {
	n, err := cnj.ParseCNJ(numero)
	if err != nil {
		return nil, err
	}
	if c.APIKey == "" {
		return nil, &DataJudError{"DATAJUD_API_KEY não configurada (veja .env.example)."}
	}
	alias, err := n.AliasDataJud()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]interface{}{
		"query": map[string]interface{}{
			"match": map[string]string{"numeroProcesso": n.Limpo()},
		},
	})
	url := fmt.Sprintf("%s/api_publica_%s/_search", c.BaseURL, alias)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "APIKey "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &DataJudError{fmt.Sprintf("DataJud respondeu HTTP %d", resp.StatusCode)}
	}
	var envelope struct {
		Hits struct {
			Hits []datajudHit `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if len(envelope.Hits.Hits) == 0 {
		return nil, nil
	}
	p := normalizar(envelope.Hits.Hits[0])
	return &p, nil
}
