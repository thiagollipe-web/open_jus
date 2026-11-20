package aprender_test

import (
	"path/filepath"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/aprender"
)

func TestAprendizadoRecorrente(t *testing.T) {
	db := filepath.Join(t.TempDir(), "a.db")
	p := "Qual o horário de atendimento do fórum trabalhista?"
	r := "Consulte o site do TRT."

	// Nada ensinado: sem resposta.
	if got, _ := aprender.Buscar(db, p, 0.6); got != "" {
		t.Fatalf("resposta antes de ensinar: %q", got)
	}
	id, err := aprender.Ensinar(db, p, r)
	if err != nil {
		t.Fatalf("Ensinar: %v", err)
	}
	// Pendente não vale.
	if got, _ := aprender.Buscar(db, p, 0.6); got != "" {
		t.Fatalf("resposta pendente não devia valer: %q", got)
	}
	pend, err := aprender.Pendentes(db)
	if err != nil || len(pend) != 1 || pend[0].ID != id {
		t.Fatalf("pendentes = %+v, %v", pend, err)
	}
	ok, err := aprender.Aprovar(db, id)
	if err != nil || !ok {
		t.Fatalf("Aprovar: %v, %v", ok, err)
	}
	// Pergunta parecida encontra a resposta aprovada (Jaccard >= 0.6).
	got, err := aprender.Buscar(db, "horário de atendimento do fórum trabalhista", 0.6)
	if err != nil || got != r {
		t.Fatalf("Buscar = %q, %v; quero %q", got, err, r)
	}
	// Aprovar item inexistente.
	if ok, _ := aprender.Aprovar(db, 9999); ok {
		t.Fatal("aprovar item inexistente devia dar false")
	}
}

func TestLacunas(t *testing.T) {
	db := filepath.Join(t.TempDir(), "l.db")
	if err := aprender.RegistrarLacuna(db, "pergunta sem resposta"); err != nil {
		t.Fatalf("RegistrarLacuna: %v", err)
	}
	if err := aprender.RegistrarLacuna(db, "pergunta sem resposta"); err != nil {
		t.Fatalf("RegistrarLacuna: %v", err)
	}
	lac, err := aprender.Lacunas(db, 10)
	if err != nil || len(lac) != 1 || lac[0].N != 2 {
		t.Fatalf("lacunas = %+v, %v", lac, err)
	}
}

func TestSimilaridade(t *testing.T) {
	// Tokens de até 2 letras são descartados (mesmo contrato do Python),
	// por isso o teste usa palavras reais.
	if s := aprender.Similaridade("prazo civil penal", "prazo civil penal"); s != 1 {
		t.Fatalf("similaridade idêntica = %v, quero 1", s)
	}
	if s := aprender.Similaridade("prazo civil penal", "recurso agravo embargo"); s != 0 {
		t.Fatalf("similaridade disjunta = %v, quero 0", s)
	}
	if s := aprender.Similaridade("prazo civil penal", "prazo civil"); s != 2.0/3.0 {
		t.Fatalf("similaridade parcial = %v, quero 2/3", s)
	}
}
