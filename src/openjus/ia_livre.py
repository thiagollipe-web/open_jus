"""IA gratuita via qualquer API compatível com OpenAI (/chat/completions).

Funciona com provedores que têm plano grátis (ex.: Groq, OpenRouter modelos ":free", Gemini
no endpoint compatível) ou um agregador próprio (ex.: freellmapi). A chave fica só no servidor.
Com trechos de lei: resume ancorado neles. Sem trechos: orientação geral, marcada como sem fonte.
"""

from __future__ import annotations

import httpx

from .config import get_settings

SISTEMA = (
    "Você é o OpenJus, assistente jurídico informativo do Brasil. Responda em português simples, "
    "curto e direto. Se houver CONTEXTO, use SOMENTE ele e não acrescente leis. Se não houver "
    "contexto, dê só orientação geral, NÃO cite números de artigos, leis ou prazos específicos e "
    "recomende confirmar com um advogado ou a Defensoria Pública. Nunca invente fatos."
)


def configurada() -> bool:
    s = get_settings()
    return bool(s.free_llm_base_url and s.free_llm_model)


async def perguntar(pergunta: str, trechos: list[str],
                    client: httpx.AsyncClient | None = None) -> str | None:
    s = get_settings()
    if not configurada():
        return None
    user = (("CONTEXTO:\n- " + "\n- ".join(trechos) + "\n\n") if trechos else "") + f"PERGUNTA: {pergunta}"
    body = {"model": s.free_llm_model, "temperature": 0.2, "max_tokens": 500,
            "messages": [{"role": "system", "content": SISTEMA}, {"role": "user", "content": user}]}
    headers = {"Authorization": f"Bearer {s.free_llm_api_key}"} if s.free_llm_api_key else {}
    c = client or httpx.AsyncClient(timeout=45)
    try:
        r = await c.post(f"{s.free_llm_base_url.rstrip('/')}/chat/completions", json=body, headers=headers)
        r.raise_for_status()
        txt = r.json()["choices"][0]["message"]["content"].strip()
    except (httpx.HTTPError, KeyError, IndexError, ValueError):
        return None  # limite do plano grátis, queda etc.: o bot segue por regras
    finally:
        if client is None:
            await c.aclose()
    return txt or None
