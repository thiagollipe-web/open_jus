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
