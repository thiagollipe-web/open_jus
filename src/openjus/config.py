"""Configuração via variáveis de ambiente (.env). Nunca commitar segredos."""

from functools import lru_cache
from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    anthropic_api_key: str = ""
    anthropic_model: str = "claude-sonnet-5-5"
    ollama_url: str = ""
    ollama_model: str = "qwen2.5:0.5b"
    admin_token: str = ""
    free_daily_tokens: int = 5000
    rate_limit_per_min: int = 60
    datajud_api_key: str = ""
    datajud_base_url: str = "https://api-publica.datajud.cnj.jus.br"
    leis_dir: Path = Path("./data/leis")
    db_path: Path = Path("./data/openjus.db")
    log_level: str = "INFO"


@lru_cache
def get_settings() -> Settings:
    return Settings()
