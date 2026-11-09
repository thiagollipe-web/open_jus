"""Camada 2 (Memória) — snapshots de andamentos e sessões, com tratamento LGPD.

Processos sigilosos (nivelSigilo > 0) NÃO têm movimentos/partes persistidos: guarda-se
apenas o hash do número e o fato da consulta (Res. CNJ 331/2020 e LGPD art. 6º, III - necessidade).
"""

from __future__ import annotations

import hashlib
import json
import re
import sqlite3
from datetime import UTC, datetime
from pathlib import Path

from .datajud import ProcessoResumo
from .leis import conectar

CPF_RE = re.compile(r"\b\d{3}\.?\d{3}\.?\d{3}-?\d{2}\b")


def mascarar_pii(texto: str) -> str:
    return CPF_RE.sub("[CPF OCULTO]", texto)


def _init(con: sqlite3.Connection) -> None:
    con.executescript(
        """
        CREATE TABLE IF NOT EXISTS snapshots(
            id INTEGER PRIMARY KEY, numero_hash TEXT, numero TEXT, sigiloso INTEGER,
            capturado_em TEXT, payload TEXT);
        CREATE TABLE IF NOT EXISTS sessoes(
            id INTEGER PRIMARY KEY, sessao TEXT, evento TEXT, criado_em TEXT);
        """
    )


def salvar_snapshot(db: Path, p: ProcessoResumo) -> dict:
    con = conectar(db)
    _init(con)
    sig = p.nivel_sigilo > 0
    h = hashlib.sha256(p.numero.encode()).hexdigest()
    payload = (
        {"sigiloso": True}
        if sig
        else json.loads(p.model_dump_json(include={"classe", "assuntos", "orgao_julgador", "movimentos"}))
    )
    if not sig:
        payload["movimentos"] = [
            {**m, "nome": mascarar_pii(m["nome"])} for m in payload["movimentos"]
        ]
    con.execute(
        "INSERT INTO snapshots(numero_hash,numero,sigiloso,capturado_em,payload) VALUES(?,?,?,?,?)",
        (h, None if sig else p.numero, int(sig), datetime.now(UTC).isoformat(),
         json.dumps(payload, ensure_ascii=False)),
    )
    con.commit()
    con.close()
    return payload


def novos_movimentos(db: Path, p: ProcessoResumo) -> list[str]:
    """Compara com o último snapshot e devolve nomes de movimentos inéditos."""
    if p.nivel_sigilo > 0:
        return []
    con = conectar(db)
    _init(con)
    row = con.execute(
        "SELECT payload FROM snapshots WHERE numero=? ORDER BY id DESC LIMIT 1", (p.numero,)
    ).fetchone()
    con.close()
    if not row:
        return []
    antigos = {(m["nome"], m.get("data")) for m in json.loads(row["payload"]).get("movimentos", [])}
    return [m.nome for m in p.movimentos if (mascarar_pii(m.nome), m.data) not in antigos]


def registrar_evento(db: Path, sessao: str, evento: str) -> None:
    con = conectar(db)
    _init(con)
    con.execute("INSERT INTO sessoes(sessao,evento,criado_em) VALUES(?,?,?)",
                (sessao, evento, datetime.now(UTC).isoformat()))
    con.commit()
    con.close()
