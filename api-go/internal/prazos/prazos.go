// Package prazos calcula prazos processuais — CPC arts. 219, 220 e 224.
// Tradução direta de src/openjus/prazos.py, com o mesmo calendário
// forense offline (feriados nacionais fixos e móveis + recesso).
package prazos

import (
	"errors"
	"time"
)

// Pascoa calcula o Domingo de Páscoa (algoritmo anônimo gregoriano).
func Pascoa(ano int) time.Time {
	a, b, c := ano%19, ano/100, ano%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	mes := (h + l - 7*m + 114) / 31
	dia := (h + l - 7*m + 114) % 31 + 1
	return time.Date(ano, time.Month(mes), dia, 0, 0, 0, 0, time.UTC)
}

func dia(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }

// FeriadosNacionais devolve os feriados nacionais de um ano.
func FeriadosNacionais(ano int) map[time.Time]bool {
	p := Pascoa(ano)
	out := map[time.Time]bool{}
	for _, md := range [][2]int{{1, 1}, {4, 21}, {5, 1}, {9, 7}, {10, 12}, {11, 2}, {11, 15}, {11, 20}, {12, 25}} {
		out[dia(ano, md[0], md[1])] = true
	}
	for _, n := range []int{-48, -47, -2, 60} { // Carnaval, Sexta-feira Santa, Corpus Christi
		out[p.AddDate(0, 0, n)] = true
	}
	return out
}

// EmRecesso implementa o art. 220: 20/dez a 20/jan, inclusive.
func EmRecesso(d time.Time) bool {
	return (d.Month() == time.December && d.Day() >= 20) || (d.Month() == time.January && d.Day() <= 20)
}

// Calendario soma feriados extras (estaduais/municipais) ao calendário.
type Calendario struct {
	FeriadosExtras map[time.Time]bool
}

// DiaUtil diz se há expediente forense na data.
func (c Calendario) DiaUtil(d time.Time) bool {
	wd := d.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	if FeriadosNacionais(d.Year())[d] {
		return false
	}
	if c.FeriadosExtras != nil && c.FeriadosExtras[d] {
		return false
	}
	return !EmRecesso(d)
}

// ProximoUtil avança até o próximo dia útil (inclusive).
func (c Calendario) ProximoUtil(d time.Time) time.Time {
	for !c.DiaUtil(d) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// DataPublicacao: intimação por Diário — publicação = 1º dia útil após a disponibilização.
func (c Calendario) DataPublicacao(disponibilizacao time.Time) time.Time {
	return c.ProximoUtil(disponibilizacao.AddDate(0, 0, 1))
}

// ResultadoPrazo é o cálculo de um prazo.
type ResultadoPrazo struct {
	InicioEvento   time.Time
	InicioContagem time.Time
	Vencimento     time.Time
	Dias           int
	Fundamentos    []string
}

// CalcularPrazo calcula o termo final de um prazo a partir da intimação/publicação.
func CalcularPrazo(evento time.Time, dias int, cal Calendario, diasUteis bool) (ResultadoPrazo, error) {
	if dias <= 0 {
		return ResultadoPrazo{}, errors.New("dias deve ser positivo")
	}
	fund := []string{"CPC art. 224, caput: exclui-se o dia do começo e inclui-se o do vencimento."}
	inicio := evento.AddDate(0, 0, 1)
	var venc time.Time
	if diasUteis {
		fund = append(fund, "CPC art. 219: contagem em dias úteis.")
		inicio = cal.ProximoUtil(inicio)
		fund = append(fund, "CPC art. 224 §3º: início no primeiro dia útil; art. 220: recesso 20/12-20/01.")
		atual, restantes := inicio, dias
		restantes-- // o 1º dia útil de contagem é o primeiro dia contado
		for restantes > 0 {
			atual = cal.ProximoUtil(atual.AddDate(0, 0, 1))
			restantes--
		}
		venc = atual
	} else {
		fund = append(fund, "Prazo contado em dias corridos (ex.: prazo material).")
		venc = cal.ProximoUtil(evento.AddDate(0, 0, dias))
		fund = append(fund, "CPC art. 224 §1º: vencimento prorrogado ao próximo dia útil.")
	}
	return ResultadoPrazo{InicioEvento: evento, InicioContagem: inicio, Vencimento: venc, Dias: dias, Fundamentos: fund}, nil
}
