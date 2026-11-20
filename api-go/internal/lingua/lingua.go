// Package lingua é a camada 4 (Linguagem): traduz "juridiquês" das
// movimentações do DataJud para linguagem simples e filtra clichês de IA.
// Tradução direta de src/openjus/linguagem.py.
//
// O glossário é carregado de site/glossario.json (65 termos) — o mesmo
// arquivo que o site usa — com uma cópia embutida (go:embed) como
// fallback, para o binário funcionar mesmo sem a pasta site/ por perto.
// Um teste compara a cópia embutida com site/glossario.json do repo.
package lingua

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed glossario.json
var glossarioEmbutido []byte

// SemTraducao é o fallback de traduzir_movimento no Python original.
const SemTraducao = "Andamento processual (sem tradução cadastrada)."

// Lingua guarda o glossário carregado (ordem do JSON preservada).
type Lingua struct {
	termos map[string]string // termo -> definição
	ordem  []string          // termos na ordem do arquivo JSON
}

// CarregarGlossario lê o glossário de um caminho (site/glossario.json).
// Se path for "", usa a cópia embutida.
func CarregarGlossario(path string) (*Lingua, error) {
	var dados []byte
	var err error
	if path == "" {
		dados = glossarioEmbutido
	} else {
		dados, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	}
	return parseGlossario(dados)
}

// parseGlossario decodifica o JSON preservando a ordem das chaves
// (a ordem decide o desempate em traduzir_movimento, como o dict do Python).
func parseGlossario(dados []byte) (*Lingua, error) {
	dec := json.NewDecoder(strings.NewReader(string(dados)))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("glossário não é um objeto JSON")
	}
	l := &Lingua{termos: map[string]string{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		chave, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("chave de glossário inválida")
		}
		var valor string
		if err := dec.Decode(&valor); err != nil {
			return nil, err
		}
		l.termos[chave] = valor
		l.ordem = append(l.ordem, chave)
	}
	return l, nil
}

// Termos devolve os termos na ordem do arquivo.
func (l *Lingua) Termos() []string { return append([]string{}, l.ordem...) }

// Total devolve o número de termos.
func (l *Lingua) Total() int { return len(l.termos) }

// Definicao devolve a definição de um termo, ou "" se não existir.
func (l *Lingua) Definicao(termo string) string { return l.termos[termo] }

// Mapa devolve uma cópia do mapa termo -> definição.
func (l *Lingua) Mapa() map[string]string {
	out := make(map[string]string, len(l.termos))
	for k, v := range l.termos {
		out[k] = v
	}
	return out
}

// TraduzirMovimento devolve a explicação em linguagem simples do nome de
// uma movimentação: a definição do termo MAIS LONGO contido no texto.
// Sem termo conhecido, devolve o fallback do Python.
func (l *Lingua) TraduzirMovimento(nome string) string {
	baixo := strings.ToLower(nome)
	termos := append([]string{}, l.ordem...)
	sort.SliceStable(termos, func(i, j int) bool { return len([]rune(termos[i])) > len([]rune(termos[j])) })
	for _, t := range termos {
		if strings.Contains(baixo, t) {
			return l.termos[t]
		}
	}
	return SemTraducao
}

// TermoContido devolve o termo do glossário mais longo contido no texto,
// ou "" se nenhum estiver contido.
func (l *Lingua) TermoContido(texto string) string {
	baixo := strings.ToLower(texto)
	termos := append([]string{}, l.ordem...)
	sort.SliceStable(termos, func(i, j int) bool { return len([]rune(termos[i])) > len([]rune(termos[j])) })
	for _, t := range termos {
		if strings.Contains(baixo, t) {
			return t
		}
	}
	return ""
}

// Capitalizar imita o str.capitalize() do Python (1ª letra maiúscula,
// resto minúsculo).
func Capitalizar(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(unicode.ToUpper(r[0])) + strings.ToLower(string(r[1:]))
}

var (
	// Padrões com início ASCII: o \b do Go (RE2) equivale ao do Python.
	clichesASCII = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bcomo (um )?modelo de linguagem\b[^.]*\.\s*`),
		regexp.MustCompile(`(?i)\bem suma,?\s*`),
		regexp.MustCompile(`(?i)\bvale (ressaltar|destacar) que\s*`),
		regexp.MustCompile(`(?i)\bnão sou (um )?advogad[oa][^.]*\.\s*`),
		regexp.MustCompile(`(?i)\bespero que isso ajude[^.]*\.?\s*`),
		regexp.MustCompile(`(?i)\bespero ter ajudado[^.]*\.?\s*`),
		regexp.MustCompile(`(?i)\bclaro[!,]\s*`),
		regexp.MustCompile(`(?i)\bfico à disposição[^.]*\.?\s*`),
	}
	// Padrões que começam com letra acentuada: o \b do Go é ASCII-only e
	// falharia antes de "é"/"ó". O núcleo é casado sem \b e a borda é
	// verificada à mão (a letra anterior não pode ser letra/dígito/_).
	clicheEImportante = regexp.MustCompile(`(?i)é importante (ressaltar|destacar|notar) que\s*`)
	clicheOtima       = regexp.MustCompile(`(?i)(ótima|excelente|boa) pergunta[!.,]?\s*`)
	espacosDuplos     = regexp.MustCompile(`[ \t]{2,}`)
)

// removerComBorda remove as ocorrências do padrão cujo caractere anterior
// não seja letra, dígito ou "_" (emula o \b Unicode do Python). A
// verificação lê o texto original, então um match não "consome" o
// separador do próximo — como no re.sub do Python.
func removerComBorda(texto string, re *regexp.Regexp) string {
	locs := re.FindAllStringIndex(texto, -1)
	if len(locs) == 0 {
		return texto
	}
	var b strings.Builder
	ultimo := 0
	for _, loc := range locs {
		if loc[0] > 0 {
			prev, _ := utf8.DecodeLastRuneInString(texto[:loc[0]])
			if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' {
				continue // dentro de palavra: não remove
			}
		}
		b.WriteString(texto[ultimo:loc[0]])
		ultimo = loc[1]
	}
	b.WriteString(texto[ultimo:])
	return b.String()
}

// RemoverCliches tira os clichês de IA e colapsa espaços duplos.
// Mesmos 10 padrões de linguagem.py, na mesma ordem.
func RemoverCliches(texto string) string {
	for _, re := range clichesASCII {
		texto = re.ReplaceAllString(texto, "")
	}
	texto = removerComBorda(texto, clicheEImportante)
	texto = removerComBorda(texto, clicheOtima)
	texto = espacosDuplos.ReplaceAllString(texto, " ")
	return strings.TrimSpace(texto)
}
