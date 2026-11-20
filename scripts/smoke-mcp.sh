#!/usr/bin/env sh
# Smoke test do servidor MCP (stdio) — initialize -> tools/list -> tools/call.
# Uso (a partir da raiz do repo): sh scripts/smoke-mcp.sh
# Compila o binário, garante o índice de leis e fala JSON-RPC com ele.
# Saída: PASS/FAIL por checagem; exit 1 se qualquer uma falhar.
set -eu

cd "$(dirname "$0")/.."

BIN="$(mktemp -d)/openjus-mcp"
trap 'rm -rf "$(dirname "$BIN")"' EXIT INT TERM

echo "== build =="
go build -C api-go -o "$BIN" ./cmd/mcp
echo "binário: $BIN"

if [ ! -f data/leis.db ]; then
	echo "== índice de leis ausente, construindo =="
	sh scripts/build_leis_iniciais.sh
fi

# Banco mutável temporário (a consulta de processo grava snapshot/memória) —
# criado e removido pelo driver Python (.smoke-mutavel.db na raiz do repo).
GLOSSARIO="site/glossario.json"
[ -f "$GLOSSARIO" ] || GLOSSARIO=""

echo "== smoke stdio =="
python3 - "$BIN" <<'PYEOF'
import json, os, subprocess, sys

BIN = sys.argv[1]
env = dict(os.environ)
env["LEIS_DB_PATH"] = os.path.join(os.getcwd(), "data/leis.db")
env["DB_PATH"] = os.path.join(os.getcwd(), ".smoke-mutavel.db")
env["GLOSSARIO_PATH"] = "site/glossario.json" if os.path.exists("site/glossario.json") else ""
env["LOG_LEVEL"] = "ERROR"
env.pop("DATAJUD_API_KEY", None)

proc = subprocess.Popen([BIN], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE, env=env, text=True, bufsize=1)
fails = []
def check(nome, cond, detalhe=""):
    print(("PASS " if cond else "FAIL ") + nome + ((" — " + str(detalhe)) if detalhe and not cond else ""))
    if not cond:
        fails.append(nome)

def rpc(method, params=None, req_id=None):
    msg = {"jsonrpc": "2.0", "method": method}
    if params is not None:
        msg["params"] = params
    if req_id is not None:
        msg["id"] = req_id
    proc.stdin.write(json.dumps(msg) + "\n")
    proc.stdin.flush()

def recv():
    line = proc.stdout.readline()
    if not line:
        raise RuntimeError("servidor fechou o stdout")
    return json.loads(line)

def call(tool, args):
    rpc("tools/call", {"name": tool, "arguments": args}, req_id="c-" + tool)
    return recv()

try:
    rpc("initialize", {
        "protocolVersion": "2025-06-18",
        "capabilities": {},
        "clientInfo": {"name": "smoke-mcp", "version": "0.0.0"},
    }, req_id="init")
    r = recv()
    info = r.get("result", {}).get("serverInfo", {})
    check("initialize responde openjus", info.get("name") == "openjus", info)
    rpc("notifications/initialized")

    rpc("tools/list", {}, req_id="list")
    r = recv()
    tools = [t["name"] for t in r.get("result", {}).get("tools", [])]
    check("tools/list traz as 5 tools",
          sorted(tools) == sorted(["consultar_processo", "calcular_prazo", "buscar_legislacao",
                    "explicar_movimento", "qual_conector_usar"]), tools)
    req = {t["name"]: t.get("inputSchema", {}).get("required", [])
           for t in r.get("result", {}).get("tools", [])}
    check("obrigatórios: calcular_prazo só data_inicio+dias",
          req.get("calcular_prazo") == ["data_inicio", "dias"], req.get("calcular_prazo"))
    check("obrigatórios: buscar_legislacao só consulta",
          req.get("buscar_legislacao") == ["consulta"], req.get("buscar_legislacao"))

    r = call("calcular_prazo", {"data_inicio": "2025-03-10", "dias": 5})
    sc = r.get("result", {}).get("structuredContent", {})
    check("calcular_prazo: vencimento 2025-03-17", sc.get("vencimento") == "2025-03-17", sc)

    r = call("explicar_movimento", {"nome_movimento": "trânsito em julgado"})
    txt = "".join(c.get("text", "") for c in r.get("result", {}).get("content", []))
    check("explicar_movimento: texto puro do glossário",
          txt == "A decisão não admite mais recurso; tornou-se definitiva.", txt)

    r = call("qual_conector_usar", {"caso_de_uso": "lote"})
    sc = r.get("result", {}).get("structuredContent", {})
    check("qual_conector_usar: lote -> DataJUD_API_CALLER",
          sc.get("repo") == "DanielFillol/DataJUD_API_CALLER", sc)
    r = call("qual_conector_usar", {"caso_de_uso": "xyz"})
    check("qual_conector_usar: caso desconhecido dá erro",
          r.get("result", {}).get("isError") is True, r.get("result"))

    r = call("buscar_legislacao", {"consulta": "LGPD", "apenas_vigentes": False})
    sc = r.get("result", {}).get("structuredContent", [])
    check("buscar_legislacao: LGPD em 1º",
          len(sc) >= 1 and "13.709" in sc[0].get("titulo", ""), sc[:1])
    check("buscar_legislacao: score vai no resultado da tool",
          len(sc) >= 1 and isinstance(sc[0].get("score"), (int, float)), sc[:1])
    # Com apenas_vigentes=true (padrão) o índice real devolve 0: o campo
    # situacao fica vazio no índice (mesmo comportamento do Python).
    r = call("buscar_legislacao", {"consulta": "LGPD"})
    sc = r.get("result", {}).get("structuredContent", [])
    print("INFO vigentes=true devolve %d resultado(s) (0 esperado: situacao vazia no índice, igual ao Python)" % len(sc))

    r = call("consultar_processo", {"numero_cnj": "0801234-56.2024.8.26.0100"})
    check("consultar_processo: DV inválido dá erro",
          r.get("result", {}).get("isError") is True, r.get("result"))
    r = call("consultar_processo", {"numero_cnj": "0000001-78.2020.8.26.0100"})
    txt = "".join(c.get("text", "") for c in r.get("result", {}).get("content", []))
    check("consultar_processo: sem chave DataJud, erro claro",
          r.get("result", {}).get("isError") is True and "DATAJUD_API_KEY" in txt, txt)
finally:
    proc.stdin.close()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
    try:
        os.remove(os.path.join(os.getcwd(), ".smoke-mutavel.db"))
    except OSError:
        pass

print("== %d falha(s) ==" % len(fails))
sys.exit(1 if fails else 0)
PYEOF
