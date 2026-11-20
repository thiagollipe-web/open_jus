package cnj_test

import (
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/cnj"
)

func TestCNJValidoEAlias(t *testing.T) {
	n, err := cnj.ParseCNJ("0000001-78.2020.8.26.0100")
	if err != nil {
		t.Fatalf("ParseCNJ: %v", err)
	}
	alias, err := n.AliasDataJud()
	if err != nil {
		t.Fatalf("AliasDataJud: %v", err)
	}
	if alias != "tjsp" {
		t.Fatalf("alias = %q, quero tjsp", alias)
	}
	n2, err := cnj.ParseCNJ(n.Limpo())
	if err != nil {
		t.Fatalf("ParseCNJ(limpo): %v", err)
	}
	if n2.Formatado() != n.Formatado() {
		t.Fatalf("formatado = %q, quero %q", n2.Formatado(), n.Formatado())
	}
}

func TestCNJDVInvalido(t *testing.T) {
	if _, err := cnj.ParseCNJ("0000001-00.2020.8.26.0100"); err == nil {
		t.Fatal("esperava erro de dígito verificador")
	}
}

func TestCNJFormatoInvalido(t *testing.T) {
	if _, err := cnj.ParseCNJ("abc"); err == nil {
		t.Fatal("esperava erro de formato")
	}
}
