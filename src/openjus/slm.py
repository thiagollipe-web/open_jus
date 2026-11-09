"""SLM local opcional (Ollama): resume em linguagem simples, SOMENTE a partir de trechos recuperados.

Grátis e offline. Modelo leve padrão: qwen2.5:0.5b (`ollama pull qwen2.5:0.5b`).
Se Ollama não estiver configurado/ativo, devolve None e o bot segue por regras.
"""

from __future__ import annotations

import httpx

from .config import get_settings

SISTEMA = ("Você é o OpenJus, assistente jurídico informativo do Brasil. Responda em português "
           "simples e curto usando SOMENTE o CONTEXTO. Se o contexto não responder, diga exatamente: "
           '"Não sei responder com segurança." Não invente leis, artigos nem prazos.')


async def resumir(pergunta: str, trechos: list[str], client: httpx.AsyncClient | None = None) -> str | None:
    s = get_settings()
    if not s.ollama_url or not trechos:
        return None
    body = {"model": s.ollama_model, "stream": False, "options": {"temperature": 0},
            "messages": [{"role": "system", "content": SISTEMA},
                         {"role": "user", "content": "CONTEXTO:\n- " + "\n- ".join(trechos)
                          + f"\n\nPERGUNTA: {pergunta}"}]}
    c = client or httpx.AsyncClient(timeout=60)
    try:
        r = await c.post(f"{s.ollama_url.rstrip('/')}/api/chat", json=body)
        r.raise_for_status()
        txt = r.json()["message"]["content"].strip()
    except (httpx.HTTPError, KeyError, ValueError):
        return None
    finally:
        if client is None:
            await c.aclose()
    return None if not txt or "não sei responder" in txt.lower() else txt
