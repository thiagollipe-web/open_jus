"""Cotas e créditos do bot: franquia grátis diária + créditos pré-pagos (unidade: tokens do LLM).

Quem passa da franquia precisa ter créditos; sem saldo, a API responde 402.
Chaves são guardadas só como hash SHA-256.
"""

from __future__ import annotations

import hashlib
import secrets
import time
from collections import defaultdict, deque
from datetime import date
from pathlib import Path

from .leis import conectar

_JANELA: dict[str, deque[float]] = defaultdict(deque)


def limite_taxa(ip: str, maximo: int, janela_s: int = 60) -> bool:
    """Janela deslizante em memória (1 processo). True = permitido. Para vários workers use Redis."""
    agora = time.monotonic()
    q = _JANELA[ip]
    while q and agora - q[0] > janela_s:
        q.popleft()
    if len(q) >= maximo:
        return False
    q.append(agora)
    return True


def _h(chave: str) -> str:
    return hashlib.sha256(chave.encode()).hexdigest()


def _db(db: Path):
    con = conectar(db)
    con.execute("CREATE TABLE IF NOT EXISTS contas(h TEXT PRIMARY KEY, creditos INTEGER DEFAULT 0,"
                " gratis_usado INTEGER DEFAULT 0, dia TEXT, rotulo TEXT)")
    return con


def criar_chave(db: Path, rotulo: str = "", creditos: int = 0) -> str:
    chave = "oj_" + secrets.token_urlsafe(24)
    con = _db(db)
    con.execute("INSERT INTO contas(h,creditos,dia,rotulo) VALUES(?,?,?,?)",
                (_h(chave), creditos, date.today().isoformat(), rotulo))
    con.commit()
    con.close()
    return chave


def adicionar_creditos(db: Path, chave: str, tokens: int) -> int | None:
    con = _db(db)
    cur = con.execute("UPDATE contas SET creditos=creditos+? WHERE h=?", (tokens, _h(chave)))
    con.commit()
    novo = None
    if cur.rowcount:
        novo = con.execute("SELECT creditos FROM contas WHERE h=?", (_h(chave),)).fetchone()[0]
    con.close()
    return novo


def saldo(db: Path, chave: str, franquia: int) -> dict | None:
    """None = chave inválida. Reseta a franquia grátis a cada dia."""
    con = _db(db)
    r = con.execute("SELECT creditos,gratis_usado,dia FROM contas WHERE h=?", (_h(chave),)).fetchone()
    if r is None:
        con.close()
        return None
    hoje = date.today().isoformat()
    usado = r["gratis_usado"] if r["dia"] == hoje else 0
    if r["dia"] != hoje:
        con.execute("UPDATE contas SET gratis_usado=0, dia=? WHERE h=?", (hoje, _h(chave)))
        con.commit()
    con.close()
    return {"creditos": r["creditos"], "gratis_restante": max(0, franquia - usado)}


def debitar(db: Path, chave: str, tokens: int, franquia: int) -> None:
    """Consome primeiro a franquia grátis do dia, o excedente sai dos créditos (mínimo 0)."""
    s = saldo(db, chave, franquia)
    if s is None:
        return
    do_gratis = min(tokens, s["gratis_restante"])
    do_pago = tokens - do_gratis
    con = _db(db)
    con.execute("UPDATE contas SET gratis_usado=gratis_usado+?, creditos=MAX(0,creditos-?) WHERE h=?",
                (do_gratis, do_pago, _h(chave)))
    con.commit()
    con.close()
