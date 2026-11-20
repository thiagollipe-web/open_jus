package prazos_test

import (
	"testing"
	"time"

	"github.com/thiagollipe-web/open_jus/api-go/internal/prazos"
)

func d(ano, mes, dia int) time.Time {
	return time.Date(ano, time.Month(mes), dia, 0, 0, 0, 0, time.UTC)
}

func TestPascoa(t *testing.T) {
	if got := prazos.Pascoa(2024); !got.Equal(d(2024, 3, 31)) {
		t.Fatalf("pascoa(2024) = %v, quero 2024-03-31", got)
	}
	if got := prazos.Pascoa(2025); !got.Equal(d(2025, 4, 20)) {
		t.Fatalf("pascoa(2025) = %v, quero 2025-04-20", got)
	}
}

func TestRecesso(t *testing.T) {
	if !prazos.EmRecesso(d(2025, 12, 20)) || !prazos.EmRecesso(d(2026, 1, 20)) {
		t.Fatal("20/12 e 20/01 são recesso")
	}
	if prazos.EmRecesso(d(2026, 1, 21)) {
		t.Fatal("21/01 não é recesso")
	}
}

func TestPrazo15DiasUteisSimples(t *testing.T) {
	// intimação sexta 2025-03-07 -> início seg 10/03; 15 úteis -> 28/03
	r, err := prazos.CalcularPrazo(d(2025, 3, 7), 15, prazos.Calendario{}, true)
	if err != nil {
		t.Fatalf("CalcularPrazo: %v", err)
	}
	if !r.InicioContagem.Equal(d(2025, 3, 10)) {
		t.Fatalf("inicio_contagem = %v, quero 2025-03-10", r.InicioContagem)
	}
	if !r.Vencimento.Equal(d(2025, 3, 28)) {
		t.Fatalf("vencimento = %v, quero 2025-03-28", r.Vencimento)
	}
}

func TestPrazoAtravessaRecesso(t *testing.T) {
	r, err := prazos.CalcularPrazo(d(2025, 12, 15), 5, prazos.Calendario{}, true)
	if err != nil {
		t.Fatalf("CalcularPrazo: %v", err)
	}
	// 16,17,18,19 contam (4); recesso até 20/01; 5º dia útil = 21/01/2026
	if !r.Vencimento.Equal(d(2026, 1, 21)) {
		t.Fatalf("vencimento = %v, quero 2026-01-21", r.Vencimento)
	}
}

func TestPrazoFeriadoExtra(t *testing.T) {
	cal := prazos.Calendario{FeriadosExtras: map[time.Time]bool{d(2025, 3, 11): true}}
	r, err := prazos.CalcularPrazo(d(2025, 3, 10), 1, cal, true)
	if err != nil {
		t.Fatalf("CalcularPrazo: %v", err)
	}
	if !r.Vencimento.Equal(d(2025, 3, 12)) {
		t.Fatalf("vencimento = %v, quero 2025-03-12", r.Vencimento)
	}
}

func TestPublicacaoDiario(t *testing.T) {
	if got := (prazos.Calendario{}).DataPublicacao(d(2025, 3, 7)); !got.Equal(d(2025, 3, 10)) {
		t.Fatalf("data_publicacao = %v, quero 2025-03-10", got)
	}
}

func TestPrazoDiasNaoPositivo(t *testing.T) {
	if _, err := prazos.CalcularPrazo(d(2025, 3, 7), 0, prazos.Calendario{}, true); err == nil {
		t.Fatal("esperava erro para dias <= 0")
	}
}
