"""API FastAPI + serve o site estático em /."""

from __future__ import annotations

from datetime import date
from pathlib import Path

import structlog
import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field

from . import leis, servico
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
