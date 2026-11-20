package lingua_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
)

func TestTraduzirMovimento(t *testing.T) {
	l, err := lingua.CarregarGlossario("")
	if err != nil {
		t.Fatalf("CarregarGlossario: %v", err)
	}
	if got := l.TraduzirMovimento("Trânsito em julgado"); !strings.Contains(got, "definitiva") {
		t.Fatalf("tradução = %q, quero 'definitiva'", got)
	}
	if got := l.TraduzirMovimento("termo que não existe"); got != lingua.SemTraducao {
		t.Fatalf("tradução = %q, quero o fallback", got)
	}
}

func TestRemoverCliches(t *testing.T) {
	out := lingua.RemoverCliches("É importante ressaltar que o prazo vence.")
	if strings.Contains(out, "É importante ressaltar") {
		t.Fatalf("clichê não removido: %q", out)
	}
	out = lingua.RemoverCliches("Ótima pergunta! O prazo é de 15 dias. Espero ter ajudado.")
	if strings.Contains(out, "Ótima pergunta") || strings.Contains(out, "Espero ter ajudado") {
		t.Fatalf("clichês não removidos: %q", out)
	}
	// "café importante" não é o clichê "é importante": a borda protege.
	out = lingua.RemoverCliches("O café importante ressaltar que o prazo vence.")
	if !strings.Contains(out, "café") {
		t.Fatalf("falso positivo no clichê: %q", out)
	}
}

func TestGlossarioEmbutidoTem65Termos(t *testing.T) {
	l, err := lingua.CarregarGlossario("")
	if err != nil {
		t.Fatalf("CarregarGlossario: %v", err)
	}
	if l.Total() != 65 {
		t.Fatalf("total = %d, quero 65", l.Total())
	}
}

func TestGlossarioEmbutidoIgualAoDoSite(t *testing.T) {
	// A cópia embutida deve ser idêntica a site/glossario.json do repo.
	caminho := filepath.Join("..", "..", "..", "site", "glossario.json")
	bruto, err := os.ReadFile(caminho)
	if err != nil {
		t.Skipf("site/glossario.json ausente: %v", err)
	}
	var doSite map[string]string
	if err := json.Unmarshal(bruto, &doSite); err != nil {
		t.Fatalf("JSON do site inválido: %v", err)
	}
	l, err := lingua.CarregarGlossario("")
	if err != nil {
		t.Fatalf("CarregarGlossario: %v", err)
	}
	emb := l.Mapa()
	if len(emb) != len(doSite) {
		t.Fatalf("embutido tem %d termos, site tem %d", len(emb), len(doSite))
	}
	for k, v := range doSite {
		if emb[k] != v {
			t.Fatalf("termo %q diverge entre o embutido e o site", k)
		}
	}
}
