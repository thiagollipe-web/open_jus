"""Configuração via variáveis de ambiente (.env). Nunca commitar segredos."""

from functools import lru_cache
from pathlib import Path

from pydantic import model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    anthropic_api_key: str = ""
    anthropic_model: str = "claude-sonnet-5-5"
    free_llm_base_url: str = ""
    free_llm_api_key: str = ""
    free_llm_model: str = ""
    cors_origins: str = "https://thiagollipe-web.github.io"
    ollama_url: str = ""
    ollama_model: str = "qwen2.5:0.5b"
    admin_token: str = ""
    free_daily_tokens: int = 5000
    rate_limit_per_min: int = 60
    datajud_api_key: str = ""
    datajud_base_url: str = "https://api-publica.datajud.cnj.jus.br"
    leis_dir: Path = Path("./data/leis")
    # Estado mutável: memória, aprendizado, créditos e limites.
    db_path: Path = Path("./data/openjus.db")
    # Índice somente de legislação; nunca usar DB_PATH como fallback.
    leis_db_path: Path = Path("./data/leis.db")
    log_level: str = "INFO"

    @model_validator(mode="after")
    def bancos_devem_ser_distintos(self) -> "Settings":
        """Recusa configuração insegura antes de qualquer conexão SQLite ser aberta."""
        try:
            iguais = self.db_path.resolve(strict=False) == self.leis_db_path.resolve(strict=False)
            if not iguais and self.db_path.exists() and self.leis_db_path.exists():
                iguais = self.db_path.samefile(self.leis_db_path)
        except OSError:
            iguais = False
        if iguais:
            raise ValueError("LEIS_DB_PATH e DB_PATH não podem apontar para o mesmo arquivo.")
        return self


@lru_cache
def get_settings() -> Settings:
    return Settings()
