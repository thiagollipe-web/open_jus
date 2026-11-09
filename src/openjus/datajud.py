"""Cliente assíncrono da API Pública do DataJud (CNJ).

Endpoint: POST {base}/api_publica_<alias>/_search   Header: Authorization: APIKey <chave>
"""

from __future__ import annotations

from typing import Any

import httpx
from pydantic import BaseModel

from .cnj import NumeroCNJ, parse_cnj
from .config import Settings, get_settings


class Movimento(BaseModel):
    codigo: int | None = None
    nome: str
    data: str | None = None


class ProcessoResumo(BaseModel):
    numero: str
    tribunal: str | None = None
    classe: str | None = None
    assuntos: list[str] = []
    orgao_julgador: str | None = None
    grau: str | None = None
    data_ajuizamento: str | None = None
    nivel_sigilo: int = 0
    movimentos: list[Movimento] = []


class DataJudError(RuntimeError):
    pass


def normalizar(hit: dict[str, Any]) -> ProcessoResumo:
    s = hit.get("_source", hit)
    movs = sorted(
        (Movimento(codigo=m.get("codigo"), nome=m.get("nome", ""), data=m.get("dataHora"))
         for m in s.get("movimentos", [])),
        key=lambda m: m.data or "",
        reverse=True,
    )
    return ProcessoResumo(
        numero=s.get("numeroProcesso", ""),
        tribunal=s.get("tribunal"),
        classe=(s.get("classe") or {}).get("nome"),
        assuntos=[a.get("nome", "") for a in s.get("assuntos", [])],
        orgao_julgador=(s.get("orgaoJulgador") or {}).get("nome"),
        grau=s.get("grau"),
        data_ajuizamento=s.get("dataAjuizamento"),
        nivel_sigilo=int(s.get("nivelSigilo") or 0),
        movimentos=movs,
    )


class DataJudClient:
    def __init__(self, settings: Settings | None = None, client: httpx.AsyncClient | None = None):
        self.s = settings or get_settings()
        self._client = client

    async def consultar(self, numero: str) -> ProcessoResumo | None:
        cnj: NumeroCNJ = parse_cnj(numero)
        if not self.s.datajud_api_key:
            raise DataJudError("DATAJUD_API_KEY não configurada (veja .env.example).")
        url = f"{self.s.datajud_base_url}/api_publica_{cnj.alias_datajud}/_search"
        body = {"query": {"match": {"numeroProcesso": cnj.limpo}}}
        headers = {"Authorization": f"APIKey {self.s.datajud_api_key}",
                   "Content-Type": "application/json"}
        client = self._client or httpx.AsyncClient(timeout=30)
        try:
            r = await client.post(url, json=body, headers=headers)
        finally:
            if self._client is None:
                await client.aclose()
        if r.status_code != 200:
            raise DataJudError(f"DataJud respondeu HTTP {r.status_code}")
        hits = r.json().get("hits", {}).get("hits", [])
        return normalizar(hits[0]) if hits else None
