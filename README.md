<div align="center">

# ⚖️ OpenJus

**Bot jurídico open source para o Direito brasileiro.**
Legislação, processos do DataJud/CNJ e prazos do CPC — numa API, num servidor MCP e num site.

[![CI](https://github.com/thiagollipe-web/open_jus/actions/workflows/ci.yml/badge.svg)](https://github.com/thiagollipe-web/open_jus/actions/workflows/ci.yml)
![Python](https://img.shields.io/badge/python-3.11%2B-blue)
![Licença](https://img.shields.io/badge/licen%C3%A7a-MIT-green)
![MCP](https://img.shields.io/badge/MCP-compat%C3%ADvel-purple)

</div>

> ⚠️ **Aviso:** ferramenta informativa. Não substitui advogado. Confira feriados locais e a data efetiva da intimação.

## ✨ O que ele faz

| | Recurso | Detalhe |
|---|---|---|
| 📚 | **Busca em legislação** | Indexa o acervo [`ctresb/leis`](https://github.com/ctresb/leis) (Markdown + YAML) e cita URN, situação e arquivo de origem |
| 📂 | **Consulta de processos** | Número CNJ validado (dígito verificador) → DataJud → classe, assuntos, órgão e movimentações |
| 🗣️ | **Juridiquês → português** | Traduz movimentações e remove clichês de IA |
| ⏱️ | **Prazos do CPC** | Arts. 219, 220 e 224, com calendário forense offline |
| 🔒 | **LGPD** | Segredo de justiça não é gravado; CPFs são mascarados (Res. CNJ 331/2020) |
| 🤖 | **Servidor MCP** | Use no Claude Desktop, Cursor e outros agentes |

## 🧱 Arquitetura em 4 camadas

```
 Usuário / Agente de IA
        │
 ┌──────▼───────┐   API FastAPI · Servidor MCP · Site
 │ 4 Linguagem  │   tradução de movimentos · prazos (CPC)
 ├──────────────┤
 │ 3 Referências│   qual conector do ecossistema usar
 ├──────────────┤
 │ 2 Memória    │   snapshots SQLite · LGPD
 ├──────────────┤
 │ 1 Contexto   │   ctresb/leis (FTS5) · DataJud · número CNJ
 └──────────────┘
```

| Camada | Módulo | Função |
|---|---|---|
| 1. Contexto | `leis.py`, `datajud.py`, `cnj.py` | Ingestão da legislação e consulta de processos |
| 2. Memória | `memoria.py` | Histórico de andamentos e detecção de movimentos novos |
| 3. Referências | `referencias.py` | Aponta o repositório certo: DataJUD_API_CALLER (lote), api_cnj (pandas), mcp-juridico-brasil (agente), jjesusfilho/tjsp e crawler-jus (e-SAJ/scraping) |
| 4. Linguagem | `linguagem.py`, `prazos.py` | Tradução de juridiquês e cálculo de prazos |

**Como cruza leis × processo:** o DataJud devolve classe e assuntos; o OpenJus monta uma busca na legislação *vigente* (campo `situacao` do YAML) e devolve os trechos com URN para citação.

## 🚀 Início rápido

```bash
git clone https://github.com/thiagollipe-web/open_jus && cd open_jus
uv sync --extra dev
cp .env.example .env                           # preencha DATAJUD_API_KEY
git clone https://github.com/ctresb/leis data/leis
uv run openjus-ingest                          # indexa a legislação
uv run openjus-api                             # site + API em http://localhost:8000
uv run pytest                                  # testes
```

A chave do DataJud é gratuita: veja a [wiki do CNJ](https://datajud-wiki.cnj.jus.br/api-publica/acesso).

### 🤖 Servidor MCP (Claude Desktop / Cursor)

```json
{ "mcpServers": { "openjus": { "command": "uv", "args": ["run", "--directory", "/caminho/open_jus", "openjus-mcp"] } } }
```

Ferramentas: `consultar_processo`, `calcular_prazo`, `buscar_legislacao`, `explicar_movimento`, `qual_conector_usar`.

### 🌐 API

| Método | Rota | Descrição |
|---|---|---|
| GET | `/api/processo/{cnj}` | Consulta o processo e cruza com a legislação |
| GET | `/api/cnj/{cnj}` | Valida o número e mostra o tribunal |
| POST | `/api/prazo` | Calcula prazo (`inicio`, `dias`, `dias_uteis`, `feriados_extras`) |
| GET | `/api/leis?q=` | Busca na legislação |
| GET | `/api/traduzir?movimento=` | Explica uma movimentação |
| GET | `/api/conectores` | Lista os conectores do ecossistema |

## ⚠️ Limitações

- Feriados estaduais e municipais entram via `feriados_extras`.
- Sem prazo em dobro (Fazenda, MP) nem contagem em horas.
- O DataJud público tem dados parciais e atraso de atualização.
- Não há scraping de e-SAJ; use os conectores da camada 3.

## 🤝 Contribuindo

Issues e PRs são bem-vindos. Antes de abrir um PR: `uv run ruff check src tests && uv run pytest`.

## 💛 Apoie o projeto

O OpenJus é gratuito e mantido no tempo livre. Se ele te ajudou e você quiser contribuir, aceito doações via **Pix**:

<!-- TODO(dono do repo): troque abaixo pela sua chave Pix -->
**Chave Pix:** `SUA_CHAVE_PIX_AQUI`

Ajudar também vale: dê uma ⭐ no repositório, divulgue e envie sugestões.

## 📄 Licença

[MIT](LICENSE)

---

## Deploy da API no Azure App Service (Linux)
```bash
az webapp up -n <app> -g <rg> -l brazilsouth --sku B1 --os-type Linux --runtime "PYTHON:3.12"
az webapp config appsettings set -g <rg> -n <app> --settings SCM_DO_BUILD_DURING_DEPLOYMENT=true DATAJUD_API_KEY=<chave>
az webapp config set -g <rg> -n <app> --startup-file "PYTHONPATH=src gunicorn -w 2 -k uvicorn.workers.UvicornWorker -b 0.0.0.0:8000 openjus.api:app"
az webapp config appsettings set -g <rg> -n <app> --settings WEBSITES_PORT=8000
```
Rode `az login` antes. Para o site no GitHub Pages falar com a API, hospede a API e aponte o front para ela (hoje o Pages só calcula prazos, no navegador).
