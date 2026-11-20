#!/usr/bin/env sh
# Build reproduzível e sem rede: indexa somente os textos versionados em data/leis_iniciais.
# Não altera nem cria DB_PATH (memória, créditos, ensino e demais estado mutável).
set -eu

ACERVO="${LEIS_INICIAIS_DIR:-data/leis_iniciais}"
BANCO="${LEIS_DB_PATH:-data/leis.db}"
MUTAVEL="${DB_PATH:-data/openjus.db}"
MANIFEST="${LEIS_FONTE_MANIFEST:-data/FONTE_LEIS.json}"

[ -d "$ACERVO" ] || { echo "Acervo inicial ausente: $ACERVO" >&2; exit 1; }
[ -f "$MANIFEST" ] || { echo "Manifesto de fonte ausente: $MANIFEST" >&2; exit 1; }

# Recusa colisão textual, por caminho resolvido ou por hardlink/symlink existente.
python - "$BANCO" "$MUTAVEL" <<'PY'
import os
import sys
from pathlib import Path

leis, mutavel = map(Path, sys.argv[1:])
try:
    iguais = leis.resolve(strict=False) == mutavel.resolve(strict=False)
    if not iguais and leis.exists() and mutavel.exists():
        iguais = os.path.samefile(leis, mutavel)
except OSError:
    iguais = False
if iguais:
    raise SystemExit("Recusa de segurança: LEIS_DB_PATH e DB_PATH apontam para o mesmo arquivo.")
PY

# Um artefato de build deve ser novo: não sobrescreve índice nem banco legado por engano.
[ ! -e "$BANCO" ] || { echo "Recusa de segurança: índice de destino já existe: $BANCO" >&2; exit 1; }

# A seleção é deliberadamente pequena e auditável: só os arquivos do manifesto,
# com tamanho e SHA-256 conferidos. Para ampliar o acervo, baixe o texto da
# mesma fonte (ctresb/leis, mesmo commit), acrescente o arquivo em
# data/leis_iniciais/ e a entrada correspondente em data/FONTE_LEIS.json.
python - "$ACERVO" "$MANIFEST" <<'PY'
import hashlib
import json
import sys
from pathlib import Path

acervo, manifesto = map(Path, sys.argv[1:])
dados = json.loads(manifesto.read_text(encoding="utf-8"))
arquivos = dados.get("files", [])
if not arquivos:
    raise SystemExit("Manifesto sem documentos.")
for item in arquivos:
    caminho = acervo / item["path"]
    if not caminho.is_file():
        raise SystemExit(f"Documento ausente: {caminho}")
    bruto = caminho.read_bytes()
    if len(bruto) != item["bytes"]:
        raise SystemExit(f"Tamanho inesperado: {caminho}")
    if hashlib.sha256(bruto).hexdigest() != item["sha256"]:
        raise SystemExit(f"SHA-256 inesperado: {caminho}")
if len(list(acervo.rglob("*.md"))) != len(arquivos):
    raise SystemExit("Acervo inicial deve conter exatamente os Markdown do manifesto.")
PY

mkdir -p "$(dirname "$BANCO")"
python -m openjus.leis --dir "$ACERVO" --leis-db "$BANCO"

# Confere que o artefato contém os documentos do manifesto e ao menos um trecho por documento.
python - "$BANCO" "$MANIFEST" <<'PY'
import json
import sqlite3
import sys
from pathlib import Path

con = sqlite3.connect(sys.argv[1])
esperado = len(json.loads(Path(sys.argv[2]).read_text(encoding="utf-8")).get("files", []))
docs = con.execute("SELECT count(*) FROM leis_docs").fetchone()[0]
trechos = con.execute("SELECT count(*) FROM leis_fts").fetchone()[0]
sem_trecho = con.execute("""
    SELECT count(*) FROM leis_docs d
    LEFT JOIN (SELECT path, count(*) n FROM leis_fts GROUP BY path) f ON f.path = d.path
    WHERE coalesce(f.n, 0) = 0
""").fetchone()[0]
con.close()
if docs != esperado or trechos < esperado or sem_trecho:
    raise SystemExit(f"Validação do índice falhou: docs={docs}, trechos={trechos}, sem_trecho={sem_trecho}")
print(f"Índice inicial validado: docs={docs}, trechos={trechos}.")
PY
