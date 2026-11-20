package memoria_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/memoria"
)

func TestLGPDSigilo(t *testing.T) {
	db := filepath.Join(t.TempDir(), "m.db")
	p := &datajud.ProcessoResumo{
		Numero: "1", NivelSigilo: 5,
		Movimentos: []datajud.Movimento{{Nome: "x", Data: "2024-01-01"}},
	}
	payload, err := memoria.SalvarSnapshot(db, p)
	if err != nil {
		t.Fatalf("SalvarSnapshot: %v", err)
	}
	if len(payload) != 1 || payload["sigiloso"] != true {
		t.Fatalf("payload sigiloso = %v, quero {sigiloso: true}", payload)
	}
}

func TestMascararPII(t *testing.T) {
	if got := memoria.MascararPII("CPF 123.456.789-09"); !strings.Contains(got, "[CPF OCULTO]") {
		t.Fatalf("CPF não mascarado: %q", got)
	}
}
