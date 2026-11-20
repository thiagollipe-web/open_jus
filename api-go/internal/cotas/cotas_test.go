package cotas_test

import (
	"path/filepath"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/cotas"
)

func TestCotasFranquiaECredito(t *testing.T) {
	db := filepath.Join(t.TempDir(), "c.db")
	k, err := cotas.CriarChave(db, "t", 0)
	if err != nil {
		t.Fatalf("CriarChave: %v", err)
	}
	s, err := cotas.SaldoConta(db, k, 100)
	if err != nil || s == nil {
		t.Fatalf("SaldoConta: %v, %v", s, err)
	}
	if s.Creditos != 0 || s.GratisRestante != 100 {
		t.Fatalf("saldo = %+v, quero {0 100}", s)
	}
	if err := cotas.Debitar(db, k, 150, 100); err != nil { // estoura a franquia; sem crédito -> zera
		t.Fatalf("Debitar: %v", err)
	}
	s, _ = cotas.SaldoConta(db, k, 100)
	if s.Creditos != 0 || s.GratisRestante != 0 {
		t.Fatalf("saldo = %+v, quero {0 0}", s)
	}
	novo, err := cotas.AdicionarCreditos(db, k, 500)
	if err != nil || novo == nil || *novo != 500 {
		t.Fatalf("AdicionarCreditos = %v, %v", novo, err)
	}
	if err := cotas.Debitar(db, k, 200, 100); err != nil { // franquia esgotada: tudo sai dos créditos
		t.Fatalf("Debitar: %v", err)
	}
	s, _ = cotas.SaldoConta(db, k, 100)
	if s.Creditos != 300 {
		t.Fatalf("creditos = %d, quero 300", s.Creditos)
	}
	if s, _ := cotas.SaldoConta(db, "oj_falsa", 100); s != nil {
		t.Fatalf("chave falsa devia dar saldo nil, deu %+v", s)
	}
	if n, _ := cotas.AdicionarCreditos(db, "oj_falsa", 5); n != nil {
		t.Fatalf("chave falsa devia dar nil, deu %v", *n)
	}
}
