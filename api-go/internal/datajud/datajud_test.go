package datajud_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
)

const hitJSON = `{"hits": {"hits": [{"_source": {
	"numeroProcesso": "00000017820208260100",
	"tribunal": "TJSP",
	"classe": {"nome": "Procedimento Comum Cível"},
	"assuntos": [{"nome": "Indenização por Dano Moral"}],
	"orgaoJulgador": {"nome": "1ª Vara Cível"},
	"grau": "G1",
	"dataAjuizamento": "2020-01-15",
	"nivelSigilo": 0,
	"movimentos": [
		{"codigo": 51, "nome": "Juntada de petição", "dataHora": "2024-02-01T10:00:00"},
		{"codigo": 26, "nome": "Conclusos para despacho", "dataHora": "2024-01-20T09:00:00"}
	]}}]}}`

func TestConsultar(t *testing.T) {
	var vistoURL, vistoAuth string
	var vistoBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vistoURL = r.URL.Path
		vistoAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&vistoBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(hitJSON))
	}))
	defer srv.Close()

	c := datajud.NewClient(srv.URL, "chave-teste")
	p, err := c.Consultar(context.Background(), "0000001-78.2020.8.26.0100")
	if err != nil {
		t.Fatalf("Consultar: %v", err)
	}
	if p == nil {
		t.Fatal("esperava um processo")
	}
	if p.Classe != "Procedimento Comum Cível" {
		t.Fatalf("classe = %q", p.Classe)
	}
	if len(p.Assuntos) != 1 || p.Assuntos[0] != "Indenização por Dano Moral" {
		t.Fatalf("assuntos = %v", p.Assuntos)
	}
	// Movimentações da mais recente para a mais antiga.
	if len(p.Movimentos) != 2 || p.Movimentos[0].Nome != "Juntada de petição" {
		t.Fatalf("movimentos = %+v", p.Movimentos)
	}
	if vistoURL != "/api_publica_tjsp/_search" {
		t.Fatalf("url = %q, quero /api_publica_tjsp/_search", vistoURL)
	}
	if vistoAuth != "APIKey chave-teste" {
		t.Fatalf("auth = %q", vistoAuth)
	}
	q, _ := vistoBody["query"].(map[string]interface{})
	m, _ := q["match"].(map[string]interface{})
	if m["numeroProcesso"] != "00000017820208260100" {
		t.Fatalf("body = %v", vistoBody)
	}
}

func TestConsultarSemChave(t *testing.T) {
	c := datajud.NewClient("https://api-publica.datajud.cnj.jus.br", "")
	_, err := c.Consultar(context.Background(), "0000001-78.2020.8.26.0100")
	if err == nil || !strings.Contains(err.Error(), "DATAJUD_API_KEY não configurada") {
		t.Fatalf("erro = %v, quero falta de chave", err)
	}
}

func TestConsultarNaoEncontrado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits": {"hits": []}}`))
	}))
	defer srv.Close()
	c := datajud.NewClient(srv.URL, "k")
	p, err := c.Consultar(context.Background(), "0000001-78.2020.8.26.0100")
	if err != nil {
		t.Fatalf("Consultar: %v", err)
	}
	if p != nil {
		t.Fatalf("esperava nil, deu %+v", p)
	}
}

func TestConsultarCNJInvalido(t *testing.T) {
	c := datajud.NewClient("https://api-publica.datajud.cnj.jus.br", "k")
	if _, err := c.Consultar(context.Background(), "0000001-00.2020.8.26.0100"); err == nil {
		t.Fatal("esperava erro de CNJ")
	}
}
