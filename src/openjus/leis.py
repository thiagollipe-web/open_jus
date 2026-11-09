"""Camada 1 (Legislação) — ingestão do acervo Markdown de ctresb/leis em SQLite FTS5.

Cada arquivo .md tem cabeçalho YAML (metadados: título, URN LEX, situação, ementa, ...).
O corpo é fatiado por artigo ("Art. N") — ou por cabeçalho, se não houver artigos — e cada
fatia herda os metadados do arquivo para filtro temporal/territorial e citação precisa.
"""

from __future__ import annotations

import argparse
import json
import re
import sqlite3
from dataclasses import dataclass
from pathlib import Path

import structlog
import yaml

from .config import get_settings

log = structlog.get_logger()

FRONT_RE = re.compile(r"\A---\s*\n(.*?)\n---\s*\n", re.DOTALL)
ARTIGO_RE = re.compile(r"(?m)^(?=\s*(?:#+\s*)?\**Art\.?\s*\d+[ºo°]?[-A-Za-z]*\b)")
HEADING_RE = re.compile(r"(?m)^(?=#{1,4}\s)")


def parse_markdown(texto: str) -> tuple[dict, str]:
    m = FRONT_RE.match(texto)
    if not m:
        return {}, texto
    try:
        meta = yaml.safe_load(m.group(1)) or {}
    except yaml.YAMLError:
        meta = {}
    return (meta if isinstance(meta, dict) else {}), texto[m.end():]


def fatiar(corpo: str, max_chars: int = 2500) -> list[str]:
    partes = [p for p in ARTIGO_RE.split(corpo) if p.strip()]
    if len(partes) <= 1:
        partes = [p for p in HEADING_RE.split(corpo) if p.strip()]
    saida: list[str] = []
    for p in partes:
        p = p.strip()
        while len(p) > max_chars:  # fatia longa: corta em quebra de parágrafo
            corte = p.rfind("\n\n", 0, max_chars)
            corte = corte if corte > 0 else max_chars
            saida.append(p[:corte].strip())
            p = p[corte:].strip()
        if p:
            saida.append(p)
    return saida


def _pick(meta: dict, *chaves: str) -> str:
    for k in chaves:
        v = meta.get(k)
        if v:
            return str(v)
    return ""


def conectar(db_path: Path) -> sqlite3.Connection:
    db_path.parent.mkdir(parents=True, exist_ok=True)
    con = sqlite3.connect(db_path)
    con.row_factory = sqlite3.Row
    con.executescript(
        """
        CREATE TABLE IF NOT EXISTS leis_docs(
            path TEXT PRIMARY KEY, titulo TEXT, urn TEXT, situacao TEXT, ementa TEXT, meta TEXT);
        CREATE VIRTUAL TABLE IF NOT EXISTS leis_fts USING fts5(
            texto, path UNINDEXED, titulo UNINDEXED, urn UNINDEXED, situacao UNINDEXED,
            tokenize='unicode61 remove_diacritics 2');
        """
    )
    return con


def ingerir(leis_dir: Path, db_path: Path) -> int:
    con = conectar(db_path)
    con.execute("DELETE FROM leis_fts")
    con.execute("DELETE FROM leis_docs")
    n = 0
    for f in sorted(leis_dir.rglob("*.md")):
        meta, corpo = parse_markdown(f.read_text(encoding="utf-8", errors="replace"))
        rel = str(f.relative_to(leis_dir))
        titulo = _pick(meta, "titulo", "título", "title", "nome") or f.stem
        urn = _pick(meta, "urn", "urn_lex", "lex")
        situacao = _pick(meta, "situacao", "situação", "status")
        ementa = _pick(meta, "ementa", "descricao", "description")
        con.execute(
            "INSERT OR REPLACE INTO leis_docs VALUES(?,?,?,?,?,?)",
            (rel, titulo, urn, situacao, ementa, json.dumps(meta, default=str, ensure_ascii=False)),
        )
        for fatia in fatiar(corpo):
            con.execute(
                "INSERT INTO leis_fts(texto,path,titulo,urn,situacao) VALUES(?,?,?,?,?)",
                (fatia, rel, titulo, urn, situacao),
            )
            n += 1
    con.commit()
    con.close()
    log.info("ingestao_concluida", fatias=n, dir=str(leis_dir))
    return n


@dataclass
class Trecho:
    texto: str
    path: str
    titulo: str
    urn: str
    situacao: str
    score: float


def _fts_query(q: str) -> str:
    termos = re.findall(r"\w+", q, flags=re.UNICODE)
    return " OR ".join(f'"{t}"' for t in termos if len(t) > 2)


def buscar(db_path: Path, consulta: str, limite: int = 5, apenas_vigentes: bool = False) -> list[Trecho]:
    fq = _fts_query(consulta)
    if not fq:
        return []
    con = conectar(db_path)
    sql = ("SELECT texto,path,titulo,urn,situacao,bm25(leis_fts) AS s FROM leis_fts "
           "WHERE leis_fts MATCH ?")
    params: list = [fq]
    if apenas_vigentes:
        sql += " AND (situacao = '' OR lower(situacao) LIKE '%vigente%' OR lower(situacao) LIKE '%em vigor%')"
    sql += " ORDER BY s LIMIT ?"
    params.append(limite)
    rows = con.execute(sql, params).fetchall()
    con.close()
    return [Trecho(r["texto"], r["path"], r["titulo"], r["urn"], r["situacao"], r["s"]) for r in rows]


def cruzar_processo(db_path: Path, classe: str, assuntos: list[str], limite: int = 3) -> list[Trecho]:
    """Cruza classe/assuntos retornados pelo DataJud com a legislação indexada."""
    consulta = " ".join([classe, *assuntos])
    return buscar(db_path, consulta, limite=limite, apenas_vigentes=True)


def main() -> None:
    ap = argparse.ArgumentParser(description="Indexa o acervo ctresb/leis em SQLite FTS5")
    ap.add_argument("--dir", type=Path, default=None)
    ap.add_argument("--db", type=Path, default=None)
    a = ap.parse_args()
    s = get_settings()
    ingerir(a.dir or s.leis_dir, a.db or s.db_path)


if __name__ == "__main__":
    main()
