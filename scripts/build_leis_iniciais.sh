#!/usr/bin/env sh
# Build reproduzível e sem rede: indexa somente os textos versionados em data/leis_iniciais.
# Não altera nem cria DB_PATH (memória, créditos, ensino e demais estado mutável).
# O manifesto (SHA-256/bytes) e a recusa de sobrescrever o destino vivem no
# indexador Go (api-go/cmd/index); este script só compila e chama ele.
set -eu

ACERVO="${LEIS_INICIAIS_DIR:-data/leis_iniciais}"
BANCO="${LEIS_DB_PATH:-data/leis.db}"
MUTAVEL="${DB_PATH:-data/openjus.db}"
MANIFEST="${LEIS_FONTE_MANIFEST:-data/FONTE_LEIS.json}"

[ -d "$ACERVO" ] || { echo "Acervo inicial ausente: $ACERVO" >&2; exit 1; }
[ -f "$MANIFEST" ] || { echo "Manifesto de fonte ausente: $MANIFEST" >&2; exit 1; }

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT
go build -C api-go -o "$TMPDIR/openjus-index" ./cmd/index
"$TMPDIR/openjus-index" --dir "$ACERVO" --leis-db "$BANCO" --mutable-db "$MUTAVEL" --manifest "$MANIFEST"
