"""Bot OPCIONAL com LLM (pago): Claude interpreta a pergunta e chama as ferramentas determinísticas.

O LLM nunca calcula prazo nem cita lei de memória: usa `calcular_prazo` e `buscar_legislacao`.
"""

from __future__ import annotations

import json
from datetime import date
from typing import Any

from . import leis, servico
from .config import get_settings
from .linguagem import remover_cliches, traduzir_movimento

SYSTEM = f"""Você é o OpenJus, assistente jurídico informativo para o Direito brasileiro. Hoje é {date.today():%d/%m/%Y}.
Regras:
- Responda em português do Brasil, em linguagem simples, curta e direta.
- Prazos: SEMPRE use a ferramenta calcular_prazo. Nunca calcule de cabeça.
- Leis: SEMPRE use buscar_legislacao antes de citar texto legal; cite título e URN retornados. Se nada for encontrado, diga que não encontrou. Não invente artigos.
- Processos: use consultar_processo quando houver número CNJ. Explique as movimentações em linguagem simples.
- Você não substitui advogado; indique isso quando a pergunta pedir aconselhamento sobre caso concreto.
- Processos em segredo de justiça: não tente contornar."""

TOOLS: list[dict[str, Any]] = [
    {"name": "calcular_prazo",
     "description": "Calcula prazo processual pelo CPC (arts. 219, 220, 224). Datas AAAA-MM-DD.",
     "input_schema": {"type": "object", "properties": {
         "data_inicio": {"type": "string", "description": "Data da intimação/publicação"},
         "dias": {"type": "integer"}, "dias_uteis": {"type": "boolean", "default": True},
         "feriados_extras": {"type": "array", "items": {"type": "string"}}},
         "required": ["data_inicio", "dias"]}},
    {"name": "buscar_legislacao",
     "description": "Busca trechos na legislação indexada (ctresb/leis), com URN e situação.",
     "input_schema": {"type": "object", "properties": {
         "consulta": {"type": "string"}, "limite": {"type": "integer", "default": 5}},
         "required": ["consulta"]}},
    {"name": "consultar_processo",
     "description": "Consulta processo no DataJud pelo número CNJ (NNNNNNN-DD.AAAA.J.TT.OOOO).",
     "input_schema": {"type": "object", "properties": {"numero_cnj": {"type": "string"}},
                      "required": ["numero_cnj"]}},
    {"name": "explicar_movimento",
     "description": "Traduz o nome de uma movimentação processual para linguagem simples.",
     "input_schema": {"type": "object", "properties": {"nome_movimento": {"type": "string"}},
                      "required": ["nome_movimento"]}},
]


async def executar_ferramenta(nome: str, args: dict[str, Any]) -> Any:
    try:
        if nome == "calcular_prazo":
            extras = [date.fromisoformat(d) for d in args.get("feriados_extras", [])]
            return servico.prazo(date.fromisoformat(args["data_inicio"]), int(args["dias"]),
                                 extras, args.get("dias_uteis", True))
        if nome == "buscar_legislacao":
            db = get_settings().db_path
            if not db.exists():
                return {"erro": "Índice de leis ausente (rode openjus-ingest)."}
            return [t.__dict__ for t in leis.buscar(db, args["consulta"], args.get("limite", 5), True)]
        if nome == "consultar_processo":
            return await servico.consultar_processo(args["numero_cnj"])
        if nome == "explicar_movimento":
            return traduzir_movimento(args["nome_movimento"])
        return {"erro": f"ferramenta desconhecida: {nome}"}
    except Exception as e:  # noqa: BLE001 — erro volta ao modelo para ele se explicar
        return {"erro": f"{type(e).__name__}: {e}"}


async def conversar(mensagens: list[dict[str, Any]], client: Any = None,
                    max_passos: int = 6) -> dict[str, Any]:
    s = get_settings()
    if client is None:
        if not s.anthropic_api_key:
            raise RuntimeError("ANTHROPIC_API_KEY não configurada (veja .env.example).")
        from anthropic import AsyncAnthropic  # extra opcional: pip install openjus[llm]

        client = AsyncAnthropic(api_key=s.anthropic_api_key)
    msgs = list(mensagens)
    usadas: list[str] = []
    tokens = 0
    for _ in range(max_passos):
        r = await client.messages.create(model=s.anthropic_model, max_tokens=1500,
                                         system=SYSTEM, tools=TOOLS, messages=msgs)
        u = getattr(r, "usage", None)
        tokens += (getattr(u, "input_tokens", 0) or 0) + (getattr(u, "output_tokens", 0) or 0)
        if r.stop_reason != "tool_use":
            texto = "".join(b.text for b in r.content if b.type == "text")
            return {"resposta": remover_cliches(texto), "ferramentas": usadas, "tokens": tokens}
        msgs.append({"role": "assistant", "content": r.content})
        resultados = []
        for b in r.content:
            if b.type == "tool_use":
                usadas.append(b.name)
                out = await executar_ferramenta(b.name, b.input)
                resultados.append({"type": "tool_result", "tool_use_id": b.id,
                                   "content": json.dumps(out, ensure_ascii=False, default=str)})
        msgs.append({"role": "user", "content": resultados})
    return {"resposta": "Não consegui concluir. Reformule a pergunta.", "ferramentas": usadas,
            "tokens": tokens}
