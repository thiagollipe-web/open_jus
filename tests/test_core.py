from datetime import date

import pytest

from openjus.cnj import CNJError, parse_cnj
from openjus.datajud import normalizar
from openjus.leis import buscar, ingerir, parse_markdown
from openjus.linguagem import remover_cliches, traduzir_movimento
from openjus.memoria import mascarar_pii, salvar_snapshot
from openjus.prazos import Calendario, calcular_prazo, em_recesso, pascoa


def _cnj_valido(seq="0000001", ano="2020", j=8, tt=26, org="0100") -> str:
    dv = 98 - (int(f"{seq}{ano}{j}{tt:02d}{org}00") % 97)
    return f"{seq}-{dv:02d}.{ano}.{j}.{tt:02d}.{org}"


def test_cnj_valido_e_alias():
    n = parse_cnj(_cnj_valido())
    assert n.alias_datajud == "tjsp"
    assert parse_cnj(n.limpo).formatado == n.formatado


def test_cnj_dv_invalido():
    with pytest.raises(CNJError):
        parse_cnj("0000001-00.2020.8.26.0100")


def test_pascoa():
    assert pascoa(2024) == date(2024, 3, 31)
    assert pascoa(2025) == date(2025, 4, 20)


def test_recesso():
    assert em_recesso(date(2025, 12, 20)) and em_recesso(date(2026, 1, 20))
    assert not em_recesso(date(2026, 1, 21))


def test_prazo_15_dias_uteis_simples():
    # intimação sexta 2025-03-07 -> início seg 10/03; 15 úteis (Carnaval já passou) -> 28/03
    r = calcular_prazo(date(2025, 3, 7), 15)
    assert r.inicio_contagem == date(2025, 3, 10)
    assert r.vencimento == date(2025, 3, 28)


def test_prazo_atravessa_recesso():
    r = calcular_prazo(date(2025, 12, 15), 5)
    # 16,17,18,19 contam (4); recesso até 20/01; 5º dia útil = 21/01/2026
    assert r.vencimento == date(2026, 1, 21)


def test_prazo_feriado_extra():
    cal = Calendario({date(2025, 3, 11)})
    assert calcular_prazo(date(2025, 3, 10), 1, cal).vencimento == date(2025, 3, 12)


def test_publicacao_diario():
    assert Calendario().data_publicacao(date(2025, 3, 7)) == date(2025, 3, 10)


def test_leis_ingestao_e_busca(tmp_path):
    d = tmp_path / "leis"
    d.mkdir()
    (d / "cf.md").write_text(
        "---\ntitulo: Constituição\nurn: urn:lex:br:federal:constituicao:1988\n"
        "situacao: vigente\n---\n"
        "Art. 5º Todos são iguais perante a lei.\n\nArt. 6º São direitos sociais a educação.\n",
        encoding="utf-8")
    (d / "rev.md").write_text("---\ntitulo: Velha\nsituacao: revogada\n---\nArt. 1 Educação antiga.",
                              encoding="utf-8")
    db = tmp_path / "x.db"
    assert ingerir(d, db) == 3
    res = buscar(db, "direitos sociais educação", apenas_vigentes=True)
    assert res and all(r.titulo == "Constituição" for r in res)
    assert res[0].urn.startswith("urn:lex")
    meta, corpo = parse_markdown("sem frontmatter")
    assert meta == {} and corpo == "sem frontmatter"


def test_linguagem():
    assert "definitiva" in traduzir_movimento("Trânsito em julgado")
    assert "É importante ressaltar" not in remover_cliches("É importante ressaltar que o prazo vence.")


def test_lgpd_sigilo(tmp_path):
    p = normalizar({"_source": {"numeroProcesso": "1", "nivelSigilo": 5,
                                "movimentos": [{"nome": "x", "dataHora": "2024-01-01"}]}})
    assert salvar_snapshot(tmp_path / "m.db", p) == {"sigiloso": True}
    assert "[CPF OCULTO]" in mascarar_pii("CPF 123.456.789-09")


async def test_bot_usa_ferramenta_de_prazo():
    from types import SimpleNamespace as NS

    from openjus.bot import conversar

    class FakeMsgs:
        def __init__(self):
            self.n = 0

        async def create(self, **kw):
            self.n += 1
            if self.n == 1:
                tu = NS(type="tool_use", id="t1", name="calcular_prazo",
                        input={"data_inicio": "2025-03-07", "dias": 15})
                return NS(stop_reason="tool_use", content=[tu])
            res = kw["messages"][-1]["content"][0]["content"]
            assert "2025-03-28" in res  # modelo recebeu o resultado determinístico
            return NS(stop_reason="end_turn",
                      content=[NS(type="text", text="Vence em 28/03/2025.")])

    out = await conversar([{"role": "user", "content": "prazo?"}], client=NS(messages=FakeMsgs()))
    assert out["ferramentas"] == ["calcular_prazo"] and "28/03" in out["resposta"]


def test_cotas_franquia_e_credito(tmp_path):
    from openjus import cotas

    db = tmp_path / "c.db"
    k = cotas.criar_chave(db, "t")
    assert cotas.saldo(db, k, 100) == {"creditos": 0, "gratis_restante": 100}
    cotas.debitar(db, k, 150, 100)  # estoura a franquia; sem crédito -> zera
    assert cotas.saldo(db, k, 100) == {"creditos": 0, "gratis_restante": 0}
    assert cotas.adicionar_creditos(db, k, 500) == 500
    cotas.debitar(db, k, 200, 100)  # franquia esgotada: tudo sai dos créditos
    assert cotas.saldo(db, k, 100)["creditos"] == 300
    assert cotas.saldo(db, "oj_falsa", 100) is None
    assert cotas.adicionar_creditos(db, "oj_falsa", 5) is None


def test_api_chat_402_e_429(tmp_path, monkeypatch):
    from fastapi.testclient import TestClient

    from openjus import api, cotas
    from openjus.config import get_settings

    monkeypatch.setenv("DB_PATH", str(tmp_path / "a.db"))
    monkeypatch.setenv("ADMIN_TOKEN", "adm")
    monkeypatch.setenv("FREE_DAILY_TOKENS", "0")
    monkeypatch.setenv("RATE_LIMIT_PER_MIN", "1000")
    get_settings.cache_clear()
    c = TestClient(api.app)
    body = {"mensagens": [{"role": "user", "content": "oi"}]}
    assert c.post("/api/chat-llm", json=body).status_code == 401
    assert c.post("/admin/chaves", json={}).status_code == 403
    k = c.post("/admin/chaves", json={}, headers={"X-Admin-Token": "adm"}).json()["chave"]
    r = c.post("/api/chat-llm", json=body, headers={"X-OpenJus-Key": k})
    assert r.status_code == 402  # sem franquia e sem créditos
    monkeypatch.setenv("RATE_LIMIT_PER_MIN", "2")
    get_settings.cache_clear()
    cotas._JANELA.clear()
    codes = [c.get("/api/saude").status_code for _ in range(3)]
    assert codes == [200, 200, 429]
    get_settings.cache_clear()


async def test_bot_local_prazo_termo_e_ajuda():
    from openjus.bot_local import conversar_local

    r = await conversar_local("Fui intimado em 07/03/2025 e tenho prazo de 15 dias úteis")
    assert "28/03/2025" in r["resposta"] and r["ferramentas"] == ["calcular_prazo"]
    r = await conversar_local("o que significa trânsito em julgado?")
    assert "definitiva" in r["resposta"]
    r = await conversar_local("prazo de 10 dias")  # falta a data
    assert "data" in r["resposta"].lower()
    r = await conversar_local("0000001-00.2020.8.26.0100")  # DV inválido
    assert "inválido" in r["resposta"]
    assert (await conversar_local(""))["resposta"].startswith("Posso ajudar")


def test_api_chat_gratis_sem_chave(tmp_path, monkeypatch):
    from fastapi.testclient import TestClient

    from openjus import api
    from openjus.config import get_settings

    monkeypatch.setenv("DB_PATH", str(tmp_path / "z.db"))
    get_settings.cache_clear()
    r = TestClient(api.app).post("/api/chat", json={"mensagens": [
        {"role": "user", "content": "prazo de 5 dias a partir de 10/03/2025"}]})
    assert r.status_code == 200 and "17/03/2025" in r.json()["resposta"]
    get_settings.cache_clear()


async def test_aprendizado_recorrente(tmp_path, monkeypatch):
    from fastapi.testclient import TestClient

    from openjus import api, aprendizado
    from openjus.bot_local import conversar_local
    from openjus.config import get_settings
    from openjus.leis import ingerir

    d = tmp_path / "leis"
    d.mkdir()
    (d / "x.md").write_text("---\ntitulo: X\nsituacao: vigente\n---\nArt. 1 Nada relevante.", encoding="utf-8")
    db = tmp_path / "l.db"
    ingerir(d, db)
    monkeypatch.setenv("DB_PATH", str(db))
    monkeypatch.setenv("ADMIN_TOKEN", "adm")
    get_settings.cache_clear()
    p = "Qual o horário de atendimento do fórum trabalhista?"
    assert "ensinar:" in (await conversar_local(p))["resposta"]  # não sabe -> lacuna
    assert aprendizado.lacunas(db)[0]["n"] == 1
    c = TestClient(api.app)
    assert c.post("/api/ensinar", json={"pergunta": p, "resposta": "Consulte o site do TRT."}).json()["status"] == "pendente"
    assert "ensinar:" in (await conversar_local(p))["resposta"]  # pendente não vale
    pend = c.get("/admin/pendentes", headers={"X-Admin-Token": "adm"}).json()["pendentes"]
    assert c.post("/admin/aprovar", json={"id": pend[0]["id"]}, headers={"X-Admin-Token": "adm"}).status_code == 200
    r = await conversar_local("horário de atendimento do fórum trabalhista")
    assert "TRT" in r["resposta"] and r["ferramentas"] == ["aprendizado"]  # aprendeu
    get_settings.cache_clear()


def test_glossario_json_do_site_em_dia():
    import json
    from pathlib import Path

    from openjus.linguagem import GLOSSARIO

    j = json.loads((Path(__file__).parents[1] / "site" / "glossario.json").read_text(encoding="utf-8"))
    assert j == GLOSSARIO  # regenere site/glossario.json se mudar o glossário


async def test_slm_ollama_ancorado(monkeypatch):
    import httpx

    from openjus import slm
    from openjus.config import get_settings

    monkeypatch.setenv("OLLAMA_URL", "http://ollama.test")
    get_settings.cache_clear()
    vistos = {}

    def h(req: httpx.Request) -> httpx.Response:
        vistos["body"] = req.content.decode()
        return httpx.Response(200, json={"message": {"content": "Resumo simples."}})

    c = httpx.AsyncClient(transport=httpx.MockTransport(h))
    assert await slm.resumir("pergunta", ["trecho A"], client=c) == "Resumo simples."
    assert "trecho A" in vistos["body"] and "SOMENTE o CONTEXTO" in vistos["body"]
    assert await slm.resumir("pergunta", [], client=c) is None  # sem contexto não chama o modelo
    c2 = httpx.AsyncClient(transport=httpx.MockTransport(lambda r: httpx.Response(200, json={"message": {"content": "Não sei responder com segurança."}})))
    assert await slm.resumir("p", ["x"], client=c2) is None
    monkeypatch.setenv("OLLAMA_URL", "")
    get_settings.cache_clear()
    assert await slm.resumir("p", ["x"]) is None
