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
