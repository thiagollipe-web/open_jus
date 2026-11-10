"""API FastAPI + serve o site estático em /."""

from __future__ import annotations

import secrets
from datetime import date
from pathlib import Path

import structlog
import uvicorn
from fastapi import FastAPI, Header, HTTPException, Query, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, JSONResponse, RedirectResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field

from . import aprendizado, bot, bot_local, cotas, leis, servico
from .cnj import CNJError, parse_cnj
from .config import get_settings
from .datajud import DataJudError
from .linguagem import remover_cliches, traduzir_movimento
from .referencias import CONECTORES

log = structlog.get_logger()
app = FastAPI(title="OpenJus", version="0.1.0",
              description="Bot jurídico open source — informativo, não substitui advogado.")

app.add_middleware(
    CORSMiddleware,
    allow_origins=[o.strip() for o in get_settings().cors_origins.split(",") if o.strip()],
    allow_methods=["GET", "POST"],
    allow_headers=["Content-Type", "X-OpenJus-Key"],
)

SITE_DIR = Path(__file__).resolve().parents[2] / "site"


class PrazoIn(BaseModel):
    inicio: date
    dias: int = Field(gt=0, le=365)
    dias_uteis: bool = True
    feriados_extras: list[date] = []


class ChatIn(BaseModel):
    mensagens: list[dict] = Field(min_length=1, max_length=40)


@app.middleware("http")
async def limitar_taxa(request: Request, call_next):
    # Atrás de proxy/Azure, configure o servidor para repassar o IP real (--proxy-headers).
    if request.url.path.startswith(("/api/", "/admin/")):
        ip = request.client.host if request.client else "?"
        if not cotas.limite_taxa(ip, get_settings().rate_limit_per_min):
            return JSONResponse({"detail": "Muitas requisições. Tente em instantes."}, 429)
    return await call_next(request)


@app.post("/api/chat")
async def chat(c: ChatIn) -> dict:
    """Bot gratuito (sem LLM, sem chave): só limite por IP."""
    ultimo = next((str(m.get("content", "")) for m in reversed(c.mensagens)
                   if m.get("role") == "user"), "")
    if len(ultimo) > 1000:
        raise HTTPException(413, "Mensagem muito longa.")
    return await bot_local.conversar_local(ultimo)


class Ensino(BaseModel):
    pergunta: str = Field(min_length=5, max_length=300)
    resposta: str = Field(min_length=5, max_length=1500)


@app.post("/api/ensinar")
def ensinar(e: Ensino) -> dict:
    """Recebe ensinamento; só passa a valer depois da aprovação de um admin."""
    aprendizado.ensinar(get_settings().db_path, e.pergunta, e.resposta)
    return {"status": "pendente", "mensagem": "Obrigado! Um moderador vai revisar."}


@app.get("/admin/pendentes")
def pendentes(x_admin_token: str = Header(default="")) -> dict:
    _admin(x_admin_token)
    db = get_settings().db_path
    return {"pendentes": aprendizado.pendentes(db), "lacunas": aprendizado.lacunas(db)}


class Aprovar(BaseModel):
    id: int


@app.post("/admin/aprovar")
def aprovar(a: Aprovar, x_admin_token: str = Header(default="")) -> dict:
    _admin(x_admin_token)
    if not aprendizado.aprovar(get_settings().db_path, a.id):
        raise HTTPException(404, "Item não encontrado.")
    return {"status": "aprovado"}


@app.post("/api/chat-llm")
async def chat_llm(c: ChatIn, x_openjus_key: str = Header(default="")) -> dict:
    s = get_settings()
    if sum(len(str(m.get("content", ""))) for m in c.mensagens) > 8000:
        raise HTTPException(413, "Conversa muito longa. Comece uma nova.")
    sd = cotas.saldo(s.db_path, x_openjus_key, s.free_daily_tokens) if x_openjus_key else None
    if sd is None:
        raise HTTPException(401, "Chave de acesso ausente ou inválida (cabeçalho X-OpenJus-Key).")
    if sd["gratis_restante"] <= 0 and sd["creditos"] <= 0:
        raise HTTPException(402, "Franquia grátis do dia esgotada e sem créditos. Adquira créditos.")
    try:
        r = await bot.conversar(c.mensagens)
    except RuntimeError as e:
        raise HTTPException(503, str(e)) from e
    cotas.debitar(s.db_path, x_openjus_key, r.get("tokens", 0), s.free_daily_tokens)
    return {**r, "saldo": cotas.saldo(s.db_path, x_openjus_key, s.free_daily_tokens)}


def _admin(token: str) -> None:
    esperado = get_settings().admin_token
    if not esperado or not secrets.compare_digest(token, esperado):
        raise HTTPException(403, "Admin não autorizado.")


class NovaChave(BaseModel):
    rotulo: str = ""
    creditos: int = Field(default=0, ge=0)


class Credito(BaseModel):
    chave: str
    tokens: int = Field(gt=0)


@app.post("/admin/chaves")
def nova_chave(n: NovaChave, x_admin_token: str = Header(default="")) -> dict:
    _admin(x_admin_token)
    return {"chave": cotas.criar_chave(get_settings().db_path, n.rotulo, n.creditos)}


@app.post("/admin/creditos")
def creditar(c: Credito, x_admin_token: str = Header(default="")) -> dict:
    _admin(x_admin_token)
    novo = cotas.adicionar_creditos(get_settings().db_path, c.chave, c.tokens)
    if novo is None:
        raise HTTPException(404, "Chave não encontrada.")
    return {"creditos": novo}


@app.get("/api/status")
def status() -> dict:
    """Diagnóstico sem segredos: o que está configurado neste servidor."""
    from . import ia_livre
    from .bot_local import _tem_indice

    s = get_settings()
    return {
        "ia_gratuita_configurada": ia_livre.configurada(),
        "ia_modelo": s.free_llm_model or None,
        "ia_url": s.free_llm_base_url or None,
        "ia_chave_definida": bool(s.free_llm_api_key),
        "datajud_chave_definida": bool(s.datajud_api_key),
        "indice_de_leis": leis.indice_disponivel(s.leis_db_path),
    }


@app.get("/api/saude")
def saude() -> dict:
    return {"status": "ok"}


@app.get("/api/leis")
def buscar_leis(
    q: str,
    limite: int = Query(default=5, ge=1, le=20),
    vigentes: bool = False,
) -> list[dict]:
    """Consulta somente leitura ao índice local de legislação."""
    db = get_settings().leis_db_path
    if not leis.indice_disponivel(db):
        raise HTTPException(503, "Índice de leis ausente ou vazio. Execute o build local antes de consultar.")
    try:
        return [t.__dict__ for t in leis.buscar(db, q, limite, vigentes)]
    except leis.IndiceLeisErro as exc:
        log.warning("consulta_leis_falhou", erro=str(exc))
        raise HTTPException(503, "Índice de leis temporariamente indisponível.") from exc
    except ValueError as exc:
        raise HTTPException(422, str(exc)) from exc


@app.get("/api/processo/{numero}")
async def processo(numero: str) -> dict:
    try:
        return await servico.consultar_processo(numero)
    except CNJError as e:
        raise HTTPException(422, str(e)) from e
    except DataJudError as e:
        raise HTTPException(502, str(e)) from e


@app.get("/api/cnj/{numero}")
def cnj(numero: str) -> dict:
    try:
        n = parse_cnj(numero)
        return {"formatado": n.formatado, "tribunal_datajud": n.alias_datajud}
    except CNJError as e:
        raise HTTPException(422, str(e)) from e


@app.post("/api/prazo")
def prazo(p: PrazoIn) -> dict:
    return servico.prazo(p.inicio, p.dias, p.feriados_extras, p.dias_uteis)


@app.get("/api/traduzir")
def traduzir(movimento: str) -> dict:
    return {"explicacao": remover_cliches(traduzir_movimento(movimento))}


@app.get("/api/conectores")
def conectores() -> dict:
    return {k: v.__dict__ for k, v in CONECTORES.items()}


SITE_PUBLICO = "https://thiagollipe-web.github.io/open_jus/"


@app.get("/", response_model=None)
def index() -> FileResponse | RedirectResponse:
    # Instalado como pacote (ex.: Render) a pasta site/ não vem junto: manda para o site público.
    if (SITE_DIR / "index.html").exists():
        return FileResponse(SITE_DIR / "index.html")
    return RedirectResponse(SITE_PUBLICO)


if SITE_DIR.exists():
    app.mount("/static", StaticFiles(directory=SITE_DIR), name="static")


def main() -> None:
    uvicorn.run("openjus.api:app", host="0.0.0.0", port=8000)
