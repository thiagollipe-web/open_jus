"""Configuração via variáveis de ambiente (.env). Nunca commitar segredos."""

from functools import lru_cache
from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    datajud_api_key: str = ""
    datajud_base_url: str = "https://api-publica.datajud.cnj.jus.br"
    leis_dir: Path = Path("./data/leis")
    db_path: Path = Path("./data/openjus.db")
    log_level: str = "INFO"


@lru_cache
def get_settings() -> Settings:
    return Settings()
