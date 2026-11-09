"""Camada 3 (Referências) — qual conector do ecossistema GitHub usar em cada caso de uso."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class Conector:
    repo: str
    linguagem: str
    quando_usar: str


CONECTORES: dict[str, Conector] = {
    "lote_csv": Conector("DanielFillol/DataJUD_API_CALLER", "Go",
                         "Extração em massa assíncrona a partir de CSV de números CNJ."),
    "analise_dataframe": Conector("jespimentel/api_cnj", "Python/Pandas",
                                  "Análise estatística e DataFrames de movimentações."),
    "agente_mcp": Conector("DeHor-Labs/mcp-juridico-brasil", "Python/MCP",
                           "Automação nativa por agente de IA via servidor MCP."),
    "tjsp_esaj": Conector("jjesusfilho/tjsp", "R",
                          "TJSP/e-SAJ e API mobile quando a API REST pública falha."),
    "crawler_generico": Conector("thiagozs/crawler-jus", "NodeJS/Python",
                                 "Raspagem local de portais de tribunais sem API."),
    "legislacao": Conector("ctresb/leis", "Markdown",
                           "Acervo de legislação (CF, leis federais/estaduais) com YAML."),
}


def recomendar(caso: str) -> Conector:
    """caso: lote | dataframe | agente | tjsp | scraping | legislacao (ou chave exata)."""
    atalhos = {"lote": "lote_csv", "dataframe": "analise_dataframe", "agente": "agente_mcp",
               "tjsp": "tjsp_esaj", "esaj": "tjsp_esaj", "scraping": "crawler_generico",
               "legislacao": "legislacao"}
    chave = atalhos.get(caso, caso)
    if chave not in CONECTORES:
        raise KeyError(f"Caso desconhecido: {caso}. Opções: {sorted(CONECTORES)}")
    return CONECTORES[chave]
