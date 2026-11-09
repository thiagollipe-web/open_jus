"""Orquestra as 4 camadas — usado pela API e pelo servidor MCP."""

from __future__ import annotations

import sqlite3
from datetime import date

from . import leis, memoria
from .config import get_settings
from .datajud import DataJudClient
from .linguagem import traduzir_movimento
from .prazos import Calendario, calcular_prazo


async def consultar_processo(numero: str, client: DataJudClient | None = None) -> dict:
    s = get_settings()
    p = await (client or DataJudClient()).consultar(numero)
    if p is None:
        return {"encontrado": False}
    novos = memoria.novos_movimentos(s.db_path, p)
    memoria.salvar_snapshot(s.db_path, p)
    if p.nivel_sigilo > 0:
        return {"encontrado": True, "sigiloso": True,
                "aviso": "Processo em segredo de justiça: dados não exibidos nem armazenados."}
    legislacao = []
    if s.db_path.exists():
        try:
            legislacao = [
                {"titulo": t.titulo, "urn": t.urn, "arquivo": t.path, "trecho": t.texto[:500]}
                for t in leis.cruzar_processo(s.db_path, p.classe or "", p.assuntos)
            ]
        except sqlite3.OperationalError:  # índice ainda não criado
            legislacao = []
    return {
        "encontrado": True, "sigiloso": False,
        "classe": p.classe, "assuntos": p.assuntos, "orgao_julgador": p.orgao_julgador,
        "movimentos": [
            {"data": m.data, "nome": m.nome, "explicacao": traduzir_movimento(m.nome)}
            for m in p.movimentos[:20]
        ],
        "novos_desde_ultima_consulta": novos,
        "legislacao_relacionada": legislacao,
    }


def prazo(inicio: date, dias: int, feriados_extras: list[date] | None = None,
          dias_uteis: bool = True) -> dict:
    r = calcular_prazo(inicio, dias, Calendario(set(feriados_extras or [])), dias_uteis=dias_uteis)
    return {"inicio_evento": r.inicio_evento.isoformat(),
            "inicio_contagem": r.inicio_contagem.isoformat(),
            "vencimento": r.vencimento.isoformat(), "dias": r.dias,
            "fundamentos": list(r.fundamentos),
            "aviso": "Confira feriados locais e a data efetiva da intimação."}
