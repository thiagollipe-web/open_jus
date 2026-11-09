"""Cálculo de prazos processuais — CPC arts. 219, 220 e 224.

Regras implementadas:
- Art. 219: prazos em dias contam-se apenas dias úteis.
- Art. 220: suspensão de 20/12 a 20/01 (inclusive).
- Art. 224 §3º/caput: exclui-se o dia do começo e inclui-se o do vencimento;
  o início da contagem e o vencimento são prorrogados para o primeiro dia útil seguinte
  se caírem em dia sem expediente. (§3º: intimação via Diário -> publicação = dia útil
  seguinte à disponibilização; ver `data_publicacao`.)

Calendário forense offline: feriados nacionais fixos + móveis (Carnaval, Sexta-feira Santa,
Corpus Christi, conforme Lei 5.010/66 art. 62 e uso forense) + feriados extras informados.
Feriados estaduais/municipais devem ser passados via `feriados_extras`.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import date, timedelta
from functools import lru_cache


@lru_cache(maxsize=64)
def pascoa(ano: int) -> date:
    """Domingo de Páscoa (algoritmo anônimo gregoriano)."""
    a, b, c = ano % 19, ano // 100, ano % 100
    d, e = b // 4, b % 4
    f = (b + 8) // 25
    g = (b - f + 1) // 3
    h = (19 * a + b - d - g + 15) % 30
    i, k = c // 4, c % 4
    l = (32 + 2 * e + 2 * i - h - k) % 7
    m = (a + 11 * h + 22 * l) // 451
    mes = (h + l - 7 * m + 114) // 31
    dia = (h + l - 7 * m + 114) % 31 + 1
    return date(ano, mes, dia)


@lru_cache(maxsize=64)
def feriados_nacionais(ano: int) -> frozenset[date]:
    p = pascoa(ano)
    fixos = [(1, 1), (4, 21), (5, 1), (9, 7), (10, 12), (11, 2), (11, 15), (11, 20), (12, 25)]
    moveis = [p - timedelta(days=48), p - timedelta(days=47),  # Carnaval (seg/ter)
              p - timedelta(days=2),  # Sexta-feira Santa
              p + timedelta(days=60)]  # Corpus Christi
    return frozenset([date(ano, m, d) for m, d in fixos] + moveis)


def em_recesso(d: date) -> bool:
    """Art. 220: 20/dez a 20/jan, inclusive."""
    return (d.month == 12 and d.day >= 20) or (d.month == 1 and d.day <= 20)


@dataclass
class Calendario:
    feriados_extras: set[date] = field(default_factory=set)

    def dia_util(self, d: date) -> bool:
        return (
            d.weekday() < 5
            and d not in feriados_nacionais(d.year)
            and d not in self.feriados_extras
            and not em_recesso(d)
        )

    def proximo_util(self, d: date) -> date:
        while not self.dia_util(d):
            d += timedelta(days=1)
        return d

    def data_publicacao(self, disponibilizacao: date) -> date:
        """Intimação por Diário: publicação = 1º dia útil após a disponibilização."""
        return self.proximo_util(disponibilizacao + timedelta(days=1))


@dataclass(frozen=True)
class ResultadoPrazo:
    inicio_evento: date
    inicio_contagem: date
    vencimento: date
    dias: int
    fundamentos: tuple[str, ...]


def calcular_prazo(
    evento: date, dias: int, cal: Calendario | None = None, *, dias_uteis: bool = True
) -> ResultadoPrazo:
    """Calcula o termo final de um prazo a partir da data de início (intimação/publicação)."""
    if dias <= 0:
        raise ValueError("dias deve ser positivo")
    cal = cal or Calendario()
    fund = ["CPC art. 224, caput: exclui-se o dia do começo e inclui-se o do vencimento."]
    inicio = evento + timedelta(days=1)
    if dias_uteis:
        fund.append("CPC art. 219: contagem em dias úteis.")
        inicio = cal.proximo_util(inicio)
        fund.append("CPC art. 224 §3º: início no primeiro dia útil; art. 220: recesso 20/12-20/01.")
        atual, restantes = inicio, dias
        # o 1º dia útil de contagem é o primeiro dia contado
        restantes -= 1
        while restantes > 0:
            atual = cal.proximo_util(atual + timedelta(days=1))
            restantes -= 1
        venc = atual
    else:
        fund.append("Prazo contado em dias corridos (ex.: prazo material).")
        venc = cal.proximo_util(evento + timedelta(days=dias))
        fund.append("CPC art. 224 §1º: vencimento prorrogado ao próximo dia útil.")
    return ResultadoPrazo(evento, inicio, venc, dias, tuple(fund))
