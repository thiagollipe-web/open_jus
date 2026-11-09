"""Aprendizado recorrente SEM LLM: o bot aprende respostas ensinadas por pessoas.

Fluxo: ensinar -> fica *pendente* -> admin aprova -> passa a ser servido por similaridade.
Perguntas sem resposta viram *lacunas* (ordenadas por frequência) para o admin ensinar.
A moderação evita envenenamento do bot com informação jurídica errada.
"""

from __future__ import annotations

import re
import unicodedata
from pathlib import Path

from .leis import conectar


def tokens(txt: str) -> set[str]:
    s = unicodedata.normalize("NFD", txt.lower())
    s = "".join(c for c in s if unicodedata.category(c) != "Mn")
    return {t for t in re.findall(r"[a-z0-9]+", s) if len(t) > 2}


def similaridade(a: str, b: str) -> float:
    ta, tb = tokens(a), tokens(b)
    return len(ta & tb) / len(ta | tb) if ta and tb else 0.0


def _db(db: Path):
    con = conectar(db)
    con.executescript(
        "CREATE TABLE IF NOT EXISTS conhecimento(id INTEGER PRIMARY KEY, pergunta TEXT, resposta TEXT,"
        " aprovado INTEGER DEFAULT 0, usos INTEGER DEFAULT 0);"
        "CREATE TABLE IF NOT EXISTS lacunas(texto TEXT PRIMARY KEY, n INTEGER DEFAULT 1);")
    return con


def ensinar(db: Path, pergunta: str, resposta: str) -> int:
    con = _db(db)
    cur = con.execute("INSERT INTO conhecimento(pergunta,resposta) VALUES(?,?)",
                      (pergunta.strip()[:300], resposta.strip()[:1500]))
    con.commit()
    i = cur.lastrowid
    con.close()
    return i


def aprovar(db: Path, id_: int) -> bool:
    con = _db(db)
    cur = con.execute("UPDATE conhecimento SET aprovado=1 WHERE id=?", (id_,))
    con.commit()
    con.close()
    return cur.rowcount > 0


def pendentes(db: Path) -> list[dict]:
    con = _db(db)
    rows = [dict(r) for r in con.execute(
        "SELECT id,pergunta,resposta FROM conhecimento WHERE aprovado=0 ORDER BY id")]
    con.close()
    return rows


def buscar(db: Path, texto: str, minimo: float = 0.6) -> str | None:
    con = _db(db)
    melhor, resp, rid = 0.0, None, None
    for r in con.execute("SELECT id,pergunta,resposta FROM conhecimento WHERE aprovado=1"):
        s = similaridade(texto, r["pergunta"])
        if s > melhor:
            melhor, resp, rid = s, r["resposta"], r["id"]
    if resp and melhor >= minimo:
        con.execute("UPDATE conhecimento SET usos=usos+1 WHERE id=?", (rid,))
        con.commit()
    else:
        resp = None
    con.close()
    return resp


def registrar_lacuna(db: Path, texto: str) -> None:
    con = _db(db)
    con.execute("INSERT INTO lacunas(texto) VALUES(?) ON CONFLICT(texto) DO UPDATE SET n=n+1",
                (texto.strip()[:300],))
    con.commit()
    con.close()


def lacunas(db: Path, limite: int = 50) -> list[dict]:
    con = _db(db)
    rows = [dict(r) for r in con.execute("SELECT texto,n FROM lacunas ORDER BY n DESC LIMIT ?", (limite,))]
    con.close()
    return rows
