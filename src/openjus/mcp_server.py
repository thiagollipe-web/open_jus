"""Servidor MCP (stdio) — expõe as ferramentas do OpenJus a agentes (Claude Desktop, Cursor)."""

from __future__ import annotations

from datetime import date

from mcp.server.fastmcp import FastMCP

from . import leis, servico
from .config import get_settings
from .linguagem import traduzir_movimento
from .referencias import recomendar

mcp = FastMCP("openjus")


@mcp.tool()
async def consultar_processo(numero_cnj: str) -> dict:
    """Consulta um processo no DataJud pelo número CNJ (NNNNNNN-DD.AAAA.J.TT.OOOO)."""
    return await servico.consultar_processo(numero_cnj)


@mcp.tool()
def calcular_prazo(data_inicio: str, dias: int, dias_uteis: bool = True,
                   feriados_extras: list[str] | None = None) -> dict:
    """Calcula prazo processual (CPC arts. 219, 220, 224). Datas em AAAA-MM-DD."""
    extras = [date.fromisoformat(d) for d in feriados_extras or []]
    return servico.prazo(date.fromisoformat(data_inicio), dias, extras, dias_uteis)


@mcp.tool()
def buscar_legislacao(consulta: str, limite: int = 5, apenas_vigentes: bool = True) -> list[dict]:
    """Busca trechos na legislação indexada (ctresb/leis), com URN e situação."""
    return [t.__dict__ for t in leis.buscar(get_settings().db_path, consulta, limite, apenas_vigentes)]


@mcp.tool()
def explicar_movimento(nome_movimento: str) -> str:
    """Traduz o nome de uma movimentação processual para linguagem simples."""
    return traduzir_movimento(nome_movimento)


@mcp.tool()
def qual_conector_usar(caso_de_uso: str) -> dict:
    """Indica o repositório do ecossistema ideal (lote, dataframe, agente, tjsp, scraping)."""
    return recomendar(caso_de_uso).__dict__


def main() -> None:
    mcp.run()


if __name__ == "__main__":
    main()
