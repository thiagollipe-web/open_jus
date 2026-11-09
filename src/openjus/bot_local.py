"""Bot sem LLM e sem API paga: interpreta a pergunta por regras e chama as ferramentas locais.

Usa apenas: índice local de leis (ctresb/leis), glossário offline, calendário forense offline
e a API pública gratuita do DataJud (opcional, só para número CNJ).
"""

from __future__ import annotations

import re
from datetime import date

from . import aprendizado, ia_livre, leis, servico, slm
from .cnj import CNJError
from .config import get_settings
from .datajud import DataJudError
from .linguagem import GLOSSARIO, traduzir_movimento

CNJ_RE = re.compile(r"\b\d{7}-?\d{2}\.?\d{4}\.?\d\.?\d{2}\.?\d{4}\b")
DATA_RE = re.compile(r"\b(\d{1,2})/(\d{1,2})/(\d{4})\b|\b(\d{4})-(\d{2})-(\d{2})\b")
DIAS_RE = re.compile(r"\b(\d{1,3})\s*dias?\b", re.IGNORECASE)

AJUDA = (
    "Posso ajudar com:\n"
    "• Prazo: \"intimado em 07/03/2025, prazo de 15 dias úteis\"\n"
    "• Processo: envie o número CNJ (NNNNNNN-DD.AAAA.J.TT.OOOO)\n"
    "• Termos: \"o que significa trânsito em julgado?\"\n"
    "• Lei: \"o que diz a lei sobre prazo de contestação?\"\n"
    "Informativo; não substitui advogado."
)


def _data(texto: str) -> date | None:
    m = DATA_RE.search(texto)
    if not m:
        return None
    try:
        if m.group(1):
            return date(int(m.group(3)), int(m.group(2)), int(m.group(1)))
        return date(int(m.group(4)), int(m.group(5)), int(m.group(6)))
    except ValueError:
        return None


def _br(iso: str) -> str:
    a, m, d = iso.split("-")
    return f"{d}/{m}/{a}"


def _prazo(texto: str) -> dict:
    ini, dias = _data(texto), DIAS_RE.search(texto)
    if not ini or not dias:
        return {"resposta": "Para calcular o prazo preciso da data da intimação/publicação e do "
                            "número de dias. Ex.: \"intimado em 07/03/2025, 15 dias úteis\".",
                "ferramentas": []}
    corridos = bool(re.search(r"corrid", texto, re.IGNORECASE))
    r = servico.prazo(ini, int(dias.group(1)), dias_uteis=not corridos)
    tipo = "dias corridos" if corridos else "dias úteis"
    return {"resposta": (f"Prazo de {r['dias']} {tipo} a partir de {_br(r['inicio_evento'])}:\n"
                         f"• Começa a contar em {_br(r['inicio_contagem'])}\n"
                         f"• Vence em {_br(r['vencimento'])}\n"
                         f"Base: CPC arts. 219 (dias úteis), 220 (recesso 20/12–20/01) e 224. "
                         f"{r['aviso']}"),
            "ferramentas": ["calcular_prazo"]}


async def _processo(numero: str) -> dict:
    try:
        r = await servico.consultar_processo(numero)
    except CNJError as e:
        return {"resposta": f"Número CNJ inválido: {e}", "ferramentas": []}
    except DataJudError as e:
        return {"resposta": f"Não consegui consultar o DataJud agora: {e}", "ferramentas": []}
    if not r.get("encontrado"):
        return {"resposta": "Processo não encontrado no DataJud.", "ferramentas": ["consultar_processo"]}
    if r.get("sigiloso"):
        return {"resposta": r["aviso"], "ferramentas": ["consultar_processo"]}
    linhas = [f"Classe: {r['classe']}", f"Assuntos: {', '.join(r['assuntos']) or '—'}",
              f"Órgão: {r['orgao_julgador']}", "Últimas movimentações:"]
    for m in r["movimentos"][:5]:
        linhas.append(f"• {(m['data'] or '')[:10]} — {m['nome']}: {m['explicacao']}")
    return {"resposta": "\n".join(linhas), "ferramentas": ["consultar_processo"]}


def _termo(texto: str) -> dict | None:
    baixo = texto.lower()
    achou = [k for k in GLOSSARIO if k in baixo]
    if achou or re.search(r"\b(o que (significa|é|quer dizer)|significado)\b", baixo):
        alvo = max(achou, key=len) if achou else re.sub(r"^.*?(significa|é|quer dizer)\s*", "", baixo)
        return {"resposta": f"{alvo.capitalize()}: {traduzir_movimento(alvo)}",
                "ferramentas": ["explicar_movimento"]}
    return None


def _tem_indice(db) -> bool:
    import sqlite3
    try:
        con = sqlite3.connect(db)
        ok = con.execute("SELECT 1 FROM leis_fts LIMIT 1").fetchone() is not None
        con.close()
        return ok
    except sqlite3.Error:
        return False


PEDE_CNJ = ("Para consultar um processo, envie o número CNJ completo, no formato "
            "NNNNNNN-DD.AAAA.J.TT.OOOO (ex.: 0000000-00.2024.8.26.0100). Ele aparece na "
            "intimação, no site do tribunal ou com seu advogado.")


def _lei(texto: str) -> dict:
    db = get_settings().db_path
    if not db.exists() or not _tem_indice(db):
        return {"resposta": "Ainda não sei responder isso com segurança. Tente reformular, ou me "
                            "ensine com `ensinar: pergunta => resposta`.\n\n" + AJUDA,
                "ferramentas": []}
    achados = leis.buscar(db, texto, limite=3, apenas_vigentes=True)
    if not achados:
        aprendizado.registrar_lacuna(db, texto)  # vira pauta para alguém ensinar
        return {"resposta": "Ainda não sei responder isso. Você pode me ensinar com "
                            "`ensinar: pergunta => resposta` (um moderador revisa antes de eu usar).\n\n"
                            + AJUDA, "ferramentas": ["buscar_legislacao"]}
    partes = [f"• {t.titulo}{f' ({t.urn})' if t.urn else ''}:\n  {t.texto[:400].strip()}…"
              for t in achados]
    return {"resposta": "Trechos encontrados (confira o texto oficial):\n" + "\n".join(partes),
            "ferramentas": ["buscar_legislacao"], "_trechos": [t.texto[:600] for t in achados]}


async def conversar_local(texto: str) -> dict:
    texto = texto.strip()
    if not texto:
        return {"resposta": AJUDA, "ferramentas": [], "tokens": 0}
    if m := CNJ_RE.search(texto):
        out = await _processo(m.group(0))
    elif re.search(r"\b(prazo|vence|vencimento|contestar|recorrer)\b", texto, re.IGNORECASE) \
            and (_data(texto) or DIAS_RE.search(texto)):
        out = _prazo(texto)
    elif re.search(r"\bprocess(o|os|ual)\b", texto, re.IGNORECASE) and len(texto) < 80:
        out = {"resposta": PEDE_CNJ, "ferramentas": []}
    elif re.fullmatch(r"\s*(oi|ol[aá]|bom dia|boa tarde|boa noite|e a[ií])[!.\s]*", texto, re.IGNORECASE):
        out = {"resposta": "Olá! " + AJUDA, "ferramentas": []}
    elif t := _termo(texto):
        out = t
    elif (db := get_settings().db_path).exists() and (a := aprendizado.buscar(db, texto)):
        out = {"resposta": a + "\n(resposta aprendida e aprovada por moderador)", "ferramentas": ["aprendizado"]}
    elif re.search(r"\b(ajuda|oi|olá|ola|menu)\b", texto, re.IGNORECASE) and len(texto) < 20:
        out = {"resposta": AJUDA, "ferramentas": []}
    else:
        out = _lei(texto)
        trechos = out.pop("_trechos", [])
        if ia_livre.configurada():
            ia = await ia_livre.perguntar(texto, trechos)
            if ia and trechos:
                out["resposta"] = f"{ia}\n(resumo por IA; confira os trechos oficiais)\n\n" + out["resposta"]
                out["ferramentas"] = [*out["ferramentas"], "ia"]
            elif ia:
                out = {"resposta": f"{ia}\n\n⚠️ Resposta geral de IA, sem fonte legal anexada. "
                                   "Confirme com um advogado ou a Defensoria Pública.",
                       "ferramentas": ["ia"]}
        elif resumo := await slm.resumir(texto, trechos):  # SLM local opcional (Ollama)
            out["resposta"] = (f"{resumo}\n(resumo por IA local; confira os trechos oficiais)\n\n"
                               + out["resposta"])
            out["ferramentas"] = [*out["ferramentas"], "slm"]
    out.pop("_trechos", None)
    return {**out, "tokens": 0}
