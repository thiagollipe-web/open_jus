"""Camada 1 (Legislação) — índice SQLite FTS5 isolado em tabelas ``leis_*``.

O índice é separado do banco mutável de memória, cotas e aprendizado. A ingestão monta
um arquivo temporário e só troca o índice após validar o acervo; também recusa usar como
destino um SQLite existente que contenha tabelas que não sejam do índice de leis.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sqlite3
import tempfile
import unicodedata
from dataclasses import dataclass
from pathlib import Path

import structlog
import yaml

from .config import get_settings

log = structlog.get_logger()

FRONT_RE = re.compile(r"\A---\s*\n(.*?)\n---\s*\n", re.DOTALL)
ARTIGO_RE = re.compile(r"(?m)^(?=\s*(?:#+\s*)?\**Art\.?\s*\d+[ºo°]?[-A-Za-z]*\b)")
HEADING_RE = re.compile(r"(?m)^(?=#{1,4}\s)")
ARQUIVOS_NAO_NORMATIVOS = {
    "readme.md", "license.md", "licence.md", "contributing.md", "changelog.md",
    "code_of_conduct.md", "security.md",
}
SITUACOES_VIGENTES = frozenset({"vigente", "em vigor", "vigente parcialmente", "parcialmente vigente"})
TABELAS_INDICE_LEIS = frozenset({
    "leis_docs", "leis_fts", "leis_fts_data", "leis_fts_idx", "leis_fts_content",
    "leis_fts_docsize", "leis_fts_config",
})


class IndiceLeisErro(RuntimeError):
    """Erro controlado de índice/consulta de legislação."""


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
        while len(p) > max_chars:
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


def _normalizar(valor: str) -> str:
    return " ".join(
        "".join(c for c in unicodedata.normalize("NFKD", valor.casefold()) if not unicodedata.combining(c)).split()
    )


def situacao_eh_vigente(situacao: str) -> bool:
    """Só aceita estados explicitamente ativos; vazio, revogado e desconhecido são excluídos."""
    return _normalizar(situacao) in SITUACOES_VIGENTES


def _mesmo_arquivo(a: Path, b: Path) -> bool:
    """Detecta colisão inclusive por symlink, sem criar nenhum caminho."""
    try:
        return a.resolve(strict=False) == b.resolve(strict=False) or (a.exists() and b.exists() and a.samefile(b))
    except OSError:
        return False


def validar_bancos_separados(leis_db_path: Path, mutable_db_path: Path | None) -> None:
    if mutable_db_path is not None and _mesmo_arquivo(leis_db_path, mutable_db_path):
        raise ValueError("LEIS_DB_PATH e DB_PATH não podem apontar para o mesmo arquivo.")


def conectar(db_path: Path) -> sqlite3.Connection:
    """Compatibilidade do app existente para o banco mutável legado.

    A busca nova nunca chama esta função: ela sempre usa ``_conectar_leitura`` no
    ``LEIS_DB_PATH`` exclusivo. Mantê-la evita alterar memória, cotas e ensino.
    """
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


def _conectar_sqlite_somente_leitura(db_path: Path) -> sqlite3.Connection:
    """Abre um SQLite existente sem criar arquivo, inclusive em paths com ``?`` ou ``#``."""
    return sqlite3.connect(db_path.resolve().as_uri() + "?mode=ro", uri=True)


def _validar_destino_indice(leis_db_path: Path) -> None:
    """Não deixa uma ingestão substituir um banco legado/mutável por engano.

    Esta defesa não depende de ``mutable_db_path``: o destino existente é inspecionado em
    modo somente leitura e pode conter apenas as tabelas ``leis_*`` do índice atual.
    """
    if not leis_db_path.exists():
        return
    if not leis_db_path.is_file():
        raise ValueError("Destino do índice de leis não é um arquivo SQLite.")
    try:
        con = _conectar_sqlite_somente_leitura(leis_db_path)
        try:
            tabelas = {
                row[0]
                for row in con.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
                )
            }
        finally:
            con.close()
    except sqlite3.Error as exc:
        raise ValueError("Destino do índice existente não é um SQLite legível; ingestão recusada.") from exc
    estranhas = sorted(tabelas - TABELAS_INDICE_LEIS)
    if estranhas:
        raise ValueError(
            "Destino do índice contém tabelas fora do esquema de leis "
            f"({', '.join(estranhas)}); ingestão recusada."
        )


def _criar_schema(con: sqlite3.Connection) -> None:
    con.executescript("""
        CREATE TABLE leis_docs(
            path TEXT PRIMARY KEY, titulo TEXT, urn TEXT, situacao TEXT, ementa TEXT, meta TEXT);
        CREATE VIRTUAL TABLE leis_fts USING fts5(
            texto, path UNINDEXED, titulo UNINDEXED, urn UNINDEXED, situacao UNINDEXED,
            tokenize='unicode61 remove_diacritics 2');
    """)


def _conectar_leitura(db_path: Path) -> sqlite3.Connection:
    """Abre o índice sem criar schema nem arquivo."""
    if not db_path.is_file():
        raise IndiceLeisErro("Índice de leis ausente.")
    try:
        con = _conectar_sqlite_somente_leitura(db_path)
    except sqlite3.Error as exc:
        raise IndiceLeisErro("Índice de leis indisponível.") from exc
    con.row_factory = sqlite3.Row
    return con


def indice_disponivel(db_path: Path) -> bool:
    """Há tabela FTS e pelo menos um trecho pesquisável, sem escrita no SQLite."""
    try:
        con = _conectar_leitura(db_path)
        try:
            return con.execute("SELECT 1 FROM leis_fts LIMIT 1").fetchone() is not None
        finally:
            con.close()
    except (IndiceLeisErro, sqlite3.Error):
        return False


def _arquivo_normativo(arquivo: Path, meta: dict, corpo: str) -> bool:
    """Evita documentação do repositório; o corpus real deverá ser avaliado antes da ingestão."""
    if arquivo.name.casefold() in ARQUIVOS_NAO_NORMATIVOS:
        return False
    # Sem metadados legais (título/URN) não há como citar com segurança e não é indexado.
    if not (_pick(meta, "titulo", "título", "title", "nome") or _pick(meta, "urn", "urn_lex", "lex")):
        return False
    return bool(corpo.strip() and fatiar(corpo))


def _carregar_acervo(leis_dir: Path) -> list[tuple[str, dict, str]]:
    if not leis_dir.is_dir():
        raise ValueError(f"Diretório do acervo não existe ou não é diretório: {leis_dir}")
    documentos: list[tuple[str, dict, str]] = []
    for arquivo in sorted(leis_dir.rglob("*.md")):
        try:
            texto = arquivo.read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            raise ValueError(f"Não foi possível ler {arquivo}: {exc}") from exc
        meta, corpo = parse_markdown(texto)
        if _arquivo_normativo(arquivo, meta, corpo):
            documentos.append((str(arquivo.relative_to(leis_dir)), meta, corpo))
    if not documentos:
        raise ValueError("Acervo sem documentos normativos Markdown com metadados legais; índice anterior preservado.")
    return documentos


def ingerir(leis_dir: Path, leis_db_path: Path, mutable_db_path: Path | None = None) -> int:
    """Constrói um novo índice de leis e o publica por troca atômica.

    O banco mutável nunca é aberto. Em erro, o índice anterior permanece intacto.
    """
    validar_bancos_separados(leis_db_path, mutable_db_path)
    _validar_destino_indice(leis_db_path)
    documentos = _carregar_acervo(leis_dir)
    leis_db_path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporario = tempfile.mkstemp(prefix=f".{leis_db_path.name}.", suffix=".tmp", dir=leis_db_path.parent)
    os.close(fd)
    tmp = Path(temporario)
    n = 0
    try:
        con = sqlite3.connect(tmp)
        try:
            _criar_schema(con)
            for rel, meta, corpo in documentos:
                titulo = _pick(meta, "titulo", "título", "title", "nome") or Path(rel).stem
                urn = _pick(meta, "urn", "urn_lex", "lex")
                situacao = _pick(meta, "situacao", "situação", "status")
                ementa = _pick(meta, "ementa", "descricao", "description")
                con.execute("INSERT INTO leis_docs VALUES(?,?,?,?,?,?)", (rel, titulo, urn, situacao, ementa, json.dumps(meta, default=str, ensure_ascii=False)))
                for fatia in fatiar(corpo):
                    con.execute("INSERT INTO leis_fts(texto,path,titulo,urn,situacao) VALUES(?,?,?,?,?)", (fatia, rel, titulo, urn, situacao))
                    n += 1
            if not n:
                raise ValueError("Acervo não produziu trechos pesquisáveis.")
            con.commit()
            if con.execute("SELECT count(*) FROM leis_docs").fetchone()[0] != len(documentos):
                raise ValueError("Índice temporário incompleto.")
        finally:
            con.close()
        os.replace(tmp, leis_db_path)
    except Exception:
        tmp.unlink(missing_ok=True)
        raise
    log.info("ingestao_concluida", fatias=n, documentos=len(documentos), dir=str(leis_dir))
    return n


@dataclass
class Trecho:
    texto: str
    path: str
    titulo: str
    urn: str
    situacao: str
    score: float
    # Metadados opcionais do arquivo de origem. Defaults preservam consumidores antigos.
    fonte: str | None = None
    versao_texto: str | None = None
    baixado_em: str | None = None


def _metadados_de_retorno(meta_json: str | None) -> tuple[str | None, str | None, str | None]:
    """Extrai somente proveniência declarada no Markdown, sem inferir vigência."""
    try:
        meta = json.loads(meta_json or "{}")
    except (TypeError, json.JSONDecodeError):
        meta = {}
    if not isinstance(meta, dict):
        meta = {}
    return (
        _pick(meta, "fonte") or None,
        _pick(meta, "versao_texto") or None,
        _pick(meta, "baixado_em") or None,
    )


def _fts_query(q: str) -> str:
    termos = re.findall(r"\w+", q, flags=re.UNICODE)
    return " OR ".join(f'"{t}"' for t in termos if len(t) > 2)


def buscar(db_path: Path, consulta: str, limite: int = 5, apenas_vigentes: bool = False) -> list[Trecho]:
    if not 1 <= limite <= 20:
        raise ValueError("limite deve estar entre 1 e 20")
    fq = _fts_query(consulta)
    if not fq:
        return []
    con = _conectar_leitura(db_path)
    # A proveniência vem do documento associado pelo path; a FTS continua contendo só o texto.
    sql = """
        SELECT f.texto, f.path, f.titulo, f.urn, f.situacao, bm25(leis_fts) AS s, d.meta
        FROM leis_fts AS f
        LEFT JOIN leis_docs AS d ON d.path = f.path
        WHERE leis_fts MATCH ?
    """
    params: list[object] = [fq]
    if apenas_vigentes:
        marcadores = sorted(SITUACOES_VIGENTES)
        sql += f" AND trim(lower(f.situacao)) IN ({','.join('?' for _ in marcadores)})"
        params.extend(marcadores)
    sql += " ORDER BY s LIMIT ?"
    params.append(limite)
    try:
        rows = con.execute(sql, params).fetchall()
    except sqlite3.Error as exc:
        raise IndiceLeisErro("Falha ao consultar o índice de leis.") from exc
    finally:
        con.close()
    return [
        Trecho(
            r["texto"], r["path"], r["titulo"], r["urn"], r["situacao"], r["s"],
            *_metadados_de_retorno(r["meta"]),
        )
        for r in rows
    ]


def cruzar_processo(db_path: Path, classe: str, assuntos: list[str], limite: int = 3) -> list[Trecho]:
    """Cruza classe/assuntos retornados pelo DataJud com legislação explicitamente vigente."""
    consulta = " ".join([classe, *assuntos])
    return buscar(db_path, consulta, limite=limite, apenas_vigentes=True)


def main() -> None:
    ap = argparse.ArgumentParser(description="Indexa um acervo local Markdown em SQLite FTS5")
    ap.add_argument("--dir", type=Path, default=None, help="Acervo local já obtido e revisado")
    ap.add_argument("--db", "--leis-db", dest="leis_db", type=Path, default=None, help="SQLite exclusivo do índice de leis")
    a = ap.parse_args()
    s = get_settings()
    ingerir(a.dir or s.leis_dir, a.leis_db or s.leis_db_path, s.db_path)


if __name__ == "__main__":
    main()
