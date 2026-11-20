// Package cnj implementa o número único CNJ (Res. CNJ 65/2008):
// NNNNNNN-DD.AAAA.J.TT.OOOO, com dígito verificador (módulo 97, ISO 7064)
// e o mapeamento para o índice da API Pública do DataJud.
package cnj

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CNJError é um número CNJ inválido ou sem índice DataJud conhecido.
type CNJError struct{ msg string }

func (e *CNJError) Error() string { return e.msg }

var cnjRE = regexp.MustCompile(`^(\d{7})-?(\d{2})\.?(\d{4})\.?(\d)\.?(\d{2})\.?(\d{4})$`)

var ufTJ = map[int]string{
	1: "tjac", 2: "tjal", 3: "tjap", 4: "tjam", 5: "tjba", 6: "tjce", 7: "tjdft", 8: "tjes",
	9: "tjgo", 10: "tjma", 11: "tjmt", 12: "tjms", 13: "tjmg", 14: "tjpa", 15: "tjpb",
	16: "tjpr", 17: "tjpe", 18: "tjpi", 19: "tjrj", 20: "tjrn", 21: "tjrs", 22: "tjro",
	23: "tjrr", 24: "tjsc", 25: "tjse", 26: "tjsp", 27: "tjto",
}

var mil = map[int]string{13: "tjmmg", 21: "tjmrs", 26: "tjmsp"} // J=9 Justiça Militar Estadual

// NumeroCNJ é um número CNJ validado.
type NumeroCNJ struct {
	Sequencial string
	DV         string
	Ano        string
	Segmento   int
	Tribunal   int
	Origem     string
}

// Formatado devolve NNNNNNN-DD.AAAA.J.TT.OOOO.
func (n NumeroCNJ) Formatado() string {
	return fmt.Sprintf("%s-%s.%s.%d.%02d.%s", n.Sequencial, n.DV, n.Ano, n.Segmento, n.Tribunal, n.Origem)
}

// Limpo devolve os 20 dígitos sem formatação.
func (n NumeroCNJ) Limpo() string {
	return fmt.Sprintf("%s%s%s%d%02d%s", n.Sequencial, n.DV, n.Ano, n.Segmento, n.Tribunal, n.Origem)
}

// AliasDataJud devolve o alias do índice na API Pública (api_publica_<alias>).
func (n NumeroCNJ) AliasDataJud() (string, error) {
	j, tt := n.Segmento, n.Tribunal
	switch {
	case j == 1:
		return "", &CNJError{"STF não é coberto pela API Pública do DataJud."}
	case j == 3 && tt == 0:
		return "stj", nil
	case j == 5 && tt == 0:
		return "tst", nil
	case j == 6 && tt == 0:
		return "tse", nil
	case j == 7 && tt == 0:
		return "stm", nil
	case j == 4 && tt >= 1 && tt <= 6:
		return fmt.Sprintf("trf%d", tt), nil
	case j == 5 && tt >= 1 && tt <= 24:
		return fmt.Sprintf("trt%d", tt), nil
	case j == 6:
		if alias, ok := ufTJ[tt]; ok {
			return "tre-" + alias[2:], nil
		}
	case j == 8:
		if alias, ok := ufTJ[tt]; ok {
			return alias, nil
		}
	case j == 9:
		if alias, ok := mil[tt]; ok {
			return alias, nil
		}
	}
	return "", &CNJError{fmt.Sprintf("Combinação J=%d TT=%02d sem índice DataJud conhecido.", j, tt)}
}

// DVValido confere o dígito verificador: DV = 98 - (NNNNNNNAAAAJTTOOOO00 mod 97).
// O resto é calculado dígito a dígito porque o número tem 20 dígitos
// (não cabe em int64).
func DVValido(sequencial, dv, ano string, j, tt int, origem string) bool {
	base := fmt.Sprintf("%s%s%d%02d%s00", sequencial, ano, j, tt, origem)
	resto := 0
	for _, ch := range base {
		resto = (resto*10 + int(ch-'0')) % 97
	}
	n, err := strconv.Atoi(dv)
	if err != nil {
		return false
	}
	return n == 98-resto
}

// ParseCNJ valida o formato e o dígito verificador.
func ParseCNJ(texto string) (NumeroCNJ, error) {
	m := cnjRE.FindStringSubmatch(strings.TrimSpace(texto))
	if m == nil {
		return NumeroCNJ{}, &CNJError{"Formato inválido. Use NNNNNNN-DD.AAAA.J.TT.OOOO."}
	}
	j, _ := strconv.Atoi(m[4])
	tt, _ := strconv.Atoi(m[5])
	n := NumeroCNJ{Sequencial: m[1], DV: m[2], Ano: m[3], Segmento: j, Tribunal: tt, Origem: m[6]}
	if !DVValido(m[1], m[2], m[3], j, tt, m[6]) {
		return NumeroCNJ{}, &CNJError{"Dígito verificador (DD) inválido."}
	}
	return n, nil
}
