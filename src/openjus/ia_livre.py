"""IA gratuita via qualquer API compatível com OpenAI (/chat/completions).

Funciona com provedores que têm plano grátis (ex.: Groq, OpenRouter modelos ":free", Gemini
no endpoint compatível) ou um agregador próprio (ex.: freellmapi). A chave fica só no servidor.
Com trechos de lei: resume ancorado neles. Sem trechos: orientação geral, marcada como sem fonte.
"""

from __future__ import annotations

import httpx

from .config import get_settings
from .linguagem import remover_cliches

SISTEMA = (
    "Você é o OpenJus, assistente jurídico informativo do Brasil. Regras OBRIGATÓRIAS:\n"
    "1. Responda SÓ o que foi perguntado, em no máximo 4 frases curtas, em português simples.\n"
    "2. Formato: uma frase de resposta direta; depois, se útil, até 2 tópicos com '•' do que fazer.\n"
    "3. Sem introdução, sem repetir a pergunta, sem conclusão, sem 'espero ter ajudado'.\n"
    "4. Se houver CONTEXTO, use SOMENTE ele. Sem contexto, NÃO cite números de artigos, leis ou "
    "prazos; dê orientação geral e diga para confirmar com advogado ou Defensoria Pública.\n"
    "5. Se a pergunta não for sobre Direito brasileiro, diga em 1 frase que só ajuda com temas "
    "jurídicos.\n"
    "6. Nunca invente fatos. Se não souber, diga: 'Não sei responder com segurança.'"
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
    body = {"model": s.free_llm_model, "temperature": 0, "max_tokens": 280,
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
    return enxugar(txt) or None


def enxugar(txt: str, max_chars: int = 900) -> str:
    """Remove clichês e corta respostas longas na última frase completa."""
    txt = remover_cliches(txt).strip()
    if len(txt) <= max_chars:
        return txt
    corte = max(txt.rfind(". ", 0, max_chars), txt.rfind("\n", 0, max_chars))
    return (txt[: corte + 1] if corte > 0 else txt[:max_chars]).strip()
