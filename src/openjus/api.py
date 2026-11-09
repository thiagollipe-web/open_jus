"""API FastAPI + serve o site estático em /."""

from __future__ import annotations

import secrets
from datetime import date
from pathlib import Path

import structlog
import uvicorn
from fastapi import FastAPI, Header, HTTPException, Request
from fastapi.responses import FileResponse, JSONResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field

from . import bot, cotas, leis, servico
from .cnj import CNJError, parse_cnj
from .config import get_settings
from .datajud import DataJudError
from .linguagem import remover_cliches, traduzir_movimento
from .referencias import CONECTORES

log = structlog.get_logger()
app = FastAPI(title="OpenJus", version="0.1.0",
              description="Bot jurídico open source — informativo, não substitui advogado.")

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
async def chat(c: ChatIn, x_openjus_key: str = Header(default="")) -> dict:
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


@app.get("/api/saude")
def saude() -> dict:
    return {"status": "ok"}


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


@app.get("/api/leis")
def buscar_leis(q: str, limite: int = 5, vigentes: bool = False) -> list[dict]:
    s = get_settings()
    if not s.db_path.exists():
        raise HTTPException(503, "Índice de leis ausente. Rode `openjus-ingest`.")
    return [t.__dict__ for t in leis.buscar(s.db_path, q, min(limite, 20), vigentes)]


@app.get("/api/traduzir")
def traduzir(movimento: str) -> dict:
    return {"explicacao": remover_cliches(traduzir_movimento(movimento))}


@app.get("/api/conectores")
def conectores() -> dict:
    return {k: v.__dict__ for k, v in CONECTORES.items()}


@app.get("/")
def index() -> FileResponse:
    return FileResponse(SITE_DIR / "index.html")


if SITE_DIR.exists():
    app.mount("/static", StaticFiles(directory=SITE_DIR), name="static")


def main() -> None:
    uvicorn.run("openjus.api:app", host="0.0.0.0", port=8000)
