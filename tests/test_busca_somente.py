"""Cobertura da busca isolada: corpus local, API e proteção do banco mutável."""

from __future__ import annotations

import hashlib
import sqlite3
from pathlib import Path

from fastapi.testclient import TestClient

from openjus import leis
from openjus.config import get_settings
from openjus.memoria import registrar_evento

RAIZ = Path(__file__).resolve().parents[1]
CORPUS_REAL = RAIZ / "data" / "leis_iniciais"


def _configurar(monkeypatch, tmp_path, nome="leis.db"):
    monkeypatch.setenv("DB_PATH", str(tmp_path / "mutavel.db"))
    monkeypatch.setenv("LEIS_DB_PATH", str(tmp_path / nome))
    monkeypatch.setenv("RATE_LIMIT_PER_MIN", "1000")
    get_settings.cache_clear()


def test_busca_prioriza_nome_da_lei_e_frase_exata(tmp_path):
    d = tmp_path / "leis"
    d.mkdir()
    (d / "a.md").write_text(
        "---\ntitulo: Lei de Direitos Sociais\nurn: urn:lex:br:federal:lei:2000-01-01;9999\n"
        "ementa: \"Garante direitos sociais.\"\nsituacao: vigente\n---\n"
        "Art. 1º Esta lei trata de orçamento.\n",
        encoding="utf-8")
    (d / "b.md").write_text(
        "---\ntitulo: Código de Processo\nurn: urn:lex:br:federal:lei:2001-01-01;8888\n"
        "ementa: \"Processo e procedimento.\"\nsituacao: vigente\n---\n"
        "Art. 1º Os direitos das partes são garantidos.\n\nArt. 2º Os interesses sociais são protegidos.\n",
        encoding="utf-8")
    db = tmp_path / "x.db"
    assert leis.ingerir(d, db) == 3
    # O nome da lei está no título/ementa, não no texto dos artigos: sem o
    # re-ranking, só os trechos que citam os termos no corpo apareceriam.
    res = leis.buscar(db, "direitos sociais", limite=2)
    assert res[0].titulo == "Lei de Direitos Sociais"
    assert res[0].urn.endswith(";9999")


def test_corpus_real_tem_manifesto_e_consultas_reprodutiveis(tmp_path):
    indice = tmp_path / "leis.db"
    assert leis.ingerir(CORPUS_REAL, indice, tmp_path / "mutavel.db") > 3

    cdc = leis.buscar(indice, "consumidor fornecedor", limite=5)
    civil = leis.buscar(indice, "casamento cônjuges", limite=5)
    cpc = leis.buscar(indice, "prazo processual dias úteis", limite=5)

    assert any(";8078" in r.urn for r in cdc)
    assert any(";10406" in r.urn for r in civil)
    assert any(";13105" in r.urn for r in cpc)
    assert all(r.fonte and r.versao_texto for r in [cdc[0], civil[0], cpc[0]])


def test_api_leis_real_e_indice_ausente(tmp_path, monkeypatch):
    from openjus import api

    _configurar(monkeypatch, tmp_path)
    client = TestClient(api.app)
    ausente = client.get("/api/leis", params={"q": "consumidor"})
    assert ausente.status_code == 503
    assert not (tmp_path / "leis.db").exists()

    leis.ingerir(CORPUS_REAL, tmp_path / "leis.db", tmp_path / "mutavel.db")
    resposta = client.get("/api/leis", params={"q": "consumidor fornecedor", "limite": 3})
    assert resposta.status_code == 200
    dados = resposta.json()
    assert dados and any(";8078" in trecho["urn"] for trecho in dados)
    assert client.get("/api/status").json()["indice_de_leis"] is True
    get_settings.cache_clear()


def test_indice_nunca_altera_banco_mutavel_sintetico(tmp_path):
    mutavel = tmp_path / "mutavel.db"
    registrar_evento(mutavel, "teste", "estado que deve sobreviver")
    antes = hashlib.sha256(mutavel.read_bytes()).hexdigest()

    indice = tmp_path / "leis.db"
    leis.ingerir(CORPUS_REAL, indice, mutavel)
    assert hashlib.sha256(mutavel.read_bytes()).hexdigest() == antes
    con = sqlite3.connect(mutavel)
    assert con.execute("SELECT evento FROM sessoes").fetchone()[0] == "estado que deve sobreviver"
    con.close()


def test_recusa_mesmo_destino_do_banco_mutavel(tmp_path):
    with __import__("pytest").raises(ValueError, match="não podem apontar"):
        leis.ingerir(CORPUS_REAL, tmp_path / "mesmo.db", tmp_path / "mesmo.db")


def test_api_tem_uma_unica_rota_leis():
    from openjus import api
    rotas = [r for r in api.app.routes if getattr(r, "path", None) == "/api/leis"]
    assert len(rotas) == 1
    assert rotas[0].endpoint.__name__ == "buscar_leis"
