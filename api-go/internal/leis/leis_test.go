package leis_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"github.com/thiagollipe-web/open_jus/api-go/internal/memoria"
)

func escrever(t *testing.T, dir, nome, conteudo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, nome), []byte(conteudo), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLeisIngestaoEBusca(t *testing.T) {
	d := filepath.Join(t.TempDir(), "leis")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	escrever(t, d, "cf.md", "---\ntitulo: Constituição\nurn: urn:lex:br:federal:constituicao:1988\n"+
		"situacao: vigente\n---\n"+
		"Art. 5º Todos são iguais perante a lei.\n\nArt. 6º São direitos sociais a educação.\n")
	escrever(t, d, "rev.md", "---\ntitulo: Velha\nsituacao: revogada\n---\nArt. 1 Educação antiga.")
	db := filepath.Join(t.TempDir(), "x.db")
	n, err := leis.Ingerir(d, db, filepath.Join(t.TempDir(), "mutavel.db"))
	if err != nil {
		t.Fatalf("Ingerir: %v", err)
	}
	if n != 3 {
		t.Fatalf("fatias = %d, quero 3", n)
	}
	res, err := leis.Buscar(db, "direitos sociais educação", 5, true)
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("esperava ao menos um trecho")
	}
	for _, r := range res {
		if r.Titulo != "Constituição" {
			t.Fatalf("titulo = %q, quero Constituição", r.Titulo)
		}
	}
	if !strings.HasPrefix(res[0].Urn, "urn:lex") {
		t.Fatalf("urn = %q, quero prefixo urn:lex", res[0].Urn)
	}
	meta, corpo := leis.ParseMarkdown("sem frontmatter")
	if len(meta) != 0 || corpo != "sem frontmatter" {
		t.Fatalf("parse_markdown = %v, %q", meta, corpo)
	}
}

func TestBuscaPriorizaNomeDaLeiEFaseExata(t *testing.T) {
	d := filepath.Join(t.TempDir(), "leis")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	escrever(t, d, "a.md", "---\ntitulo: Lei de Direitos Sociais\nurn: urn:lex:br:federal:lei:2000-01-01;9999\n"+
		"ementa: \"Garante direitos sociais.\"\nsituacao: vigente\n---\n"+
		"Art. 1º Esta lei trata de orçamento.\n")
	escrever(t, d, "b.md", "---\ntitulo: Código de Processo\nurn: urn:lex:br:federal:lei:2001-01-01;8888\n"+
		"ementa: \"Processo e procedimento.\"\nsituacao: vigente\n---\n"+
		"Art. 1º Os direitos das partes são garantidos.\n\nArt. 2º Os interesses sociais são protegidos.\n")
	db := filepath.Join(t.TempDir(), "x.db")
	n, err := leis.Ingerir(d, db, filepath.Join(t.TempDir(), "mutavel.db"))
	if err != nil {
		t.Fatalf("Ingerir: %v", err)
	}
	if n != 3 {
		t.Fatalf("fatias = %d, quero 3", n)
	}
	// O nome da lei está no título/ementa, não no texto dos artigos: sem o
	// re-ranking, só os trechos que citam os termos no corpo apareceriam.
	res, err := leis.Buscar(db, "direitos sociais", 2, false)
	if err != nil {
		t.Fatalf("Buscar: %v", err)
	}
	if len(res) == 0 || res[0].Titulo != "Lei de Direitos Sociais" {
		t.Fatalf("primeiro = %v, quero Lei de Direitos Sociais", res)
	}
	if !strings.HasSuffix(res[0].Urn, ";9999") {
		t.Fatalf("urn = %q, quero sufixo ;9999", res[0].Urn)
	}
}

func corpusReal(t *testing.T) string {
	t.Helper()
	caminho := filepath.Join("..", "..", "..", "data", "leis_iniciais")
	if st, err := os.Stat(caminho); err != nil || !st.IsDir() {
		t.Skip("corpus real ausente")
	}
	return caminho
}

func TestCorpusRealConsultasReprodutiveis(t *testing.T) {
	indice := filepath.Join(t.TempDir(), "leis.db")
	n, err := leis.Ingerir(corpusReal(t), indice, filepath.Join(t.TempDir(), "mutavel.db"))
	if err != nil {
		t.Fatalf("Ingerir: %v", err)
	}
	if n <= 3 {
		t.Fatalf("fatias = %d, quero > 3", n)
	}
	cdc, err := leis.Buscar(indice, "consumidor fornecedor", 5, false)
	if err != nil {
		t.Fatalf("Buscar CDC: %v", err)
	}
	civil, err := leis.Buscar(indice, "casamento cônjuges", 5, false)
	if err != nil {
		t.Fatalf("Buscar Civil: %v", err)
	}
	cpc, err := leis.Buscar(indice, "prazo processual dias úteis", 5, false)
	if err != nil {
		t.Fatalf("Buscar CPC: %v", err)
	}
	tem := func(res []leis.Trecho, urn string) bool {
		for _, r := range res {
			if strings.Contains(r.Urn, urn) {
				return true
			}
		}
		return false
	}
	if !tem(cdc, ";8078") {
		t.Fatalf("CDC sem ;8078: %v", urns(cdc))
	}
	if !tem(civil, ";10406") {
		t.Fatalf("Civil sem ;10406: %v", urns(civil))
	}
	if !tem(cpc, ";13105") {
		t.Fatalf("CPC sem ;13105: %v", urns(cpc))
	}
	for _, r := range []leis.Trecho{cdc[0], civil[0], cpc[0]} {
		if r.Fonte == nil || *r.Fonte == "" || r.VersaoTexto == nil || *r.VersaoTexto == "" {
			t.Fatalf("primeiro resultado sem fonte/versao_texto: %+v", r)
		}
	}
}

func urns(res []leis.Trecho) []string {
	var out []string
	for _, r := range res {
		out = append(out, r.Urn)
	}
	return out
}

func TestIndiceNuncaAlteraBancoMutavel(t *testing.T) {
	dir := t.TempDir()
	mutavel := filepath.Join(dir, "mutavel.db")
	if err := memoria.RegistrarEvento(mutavel, "teste", "estado que deve sobreviver"); err != nil {
		t.Fatalf("RegistrarEvento: %v", err)
	}
	antesBytes, err := os.ReadFile(mutavel)
	if err != nil {
		t.Fatal(err)
	}
	antes := fmt.Sprintf("%x", sha256.Sum256(antesBytes))

	indice := filepath.Join(dir, "leis.db")
	if _, err := leis.Ingerir(corpusReal(t), indice, mutavel); err != nil {
		t.Fatalf("Ingerir: %v", err)
	}
	depoisBytes, err := os.ReadFile(mutavel)
	if err != nil {
		t.Fatal(err)
	}
	if depois := fmt.Sprintf("%x", sha256.Sum256(depoisBytes)); depois != antes {
		t.Fatal("o banco mutável mudou durante a ingestão")
	}
}

func TestRecusaMesmoDestinoDoBancoMutavel(t *testing.T) {
	mesmo := filepath.Join(t.TempDir(), "mesmo.db")
	_, err := leis.Ingerir(corpusReal(t), mesmo, mesmo)
	if err == nil || !strings.Contains(err.Error(), "não podem apontar") {
		t.Fatalf("erro = %v, quero 'não podem apontar'", err)
	}
}

func TestBuscarLimiteInvalido(t *testing.T) {
	db := filepath.Join(t.TempDir(), "x.db")
	if _, err := leis.Buscar(db, "teste", 0, false); err == nil {
		t.Fatal("esperava erro para limite 0")
	}
	if _, err := leis.Buscar(db, "teste", 21, false); err == nil {
		t.Fatal("esperava erro para limite 21")
	}
}
