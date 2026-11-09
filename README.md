# OpenJus ⚖️

Bot e ecossistema web **open source** para Direito e dados jurídicos no Brasil. Híbrido: LLM para interpretar linguagem + ferramentas **determinísticas** (MCP/API) para legislação, DataJud/CNJ e prazos.

> ⚠️ Informativo. Não substitui advogado. Confira sempre feriados locais e a data efetiva da intimação.

## As 4 camadas

| Camada | Módulo | O que faz |
|---|---|---|
| 1. Contexto | `leis.py`, `datajud.py`, `cnj.py` | Indexa `ctresb/leis` (YAML + Markdown) em SQLite FTS5 fatiando por artigo; consulta DataJud por número CNJ |
| 2. Memória | `memoria.py` | Snapshots de andamentos, detecção de movimentos novos; **segredo de justiça não é persistido**; CPFs mascarados (LGPD / Res. CNJ 331/2020) |
| 3. Referências | `referencias.py` | Qual conector usar: DataJUD_API_CALLER (lote), api_cnj (pandas), mcp-juridico-brasil (agente), jjesusfilho/tjsp e crawler-jus (e-SAJ/scraping) |
| 4. Linguagem | `linguagem.py`, `prazos.py` | Traduz juridiquês, remove clichês de IA, calcula prazos (CPC arts. 219, 220, 224) com calendário forense offline |

### Como cruza `ctresb/leis` × DataJud
`servico.consultar_processo` obtém classe e assuntos no DataJud, monta uma consulta FTS5 e busca **apenas trechos vigentes** (filtro pelo campo `situacao` do YAML), devolvendo título, URN LEX e arquivo de origem para citação precisa.

## Início rápido

```bash
uv sync --extra dev          # ou: uv pip install -e ".[dev]"
cp .env.example .env         # preencha DATAJUD_API_KEY
git clone https://github.com/ctresb/leis data/leis
uv run openjus-ingest        # indexa a legislação
uv run openjus-api           # site + API em http://localhost:8000
uv run pytest
```

### Servidor MCP (Claude Desktop / Cursor)
```json
{ "mcpServers": { "openjus": { "command": "uv", "args": ["run", "--directory", "/caminho/open_jus", "openjus-mcp"] } } }
```
Ferramentas: `consultar_processo`, `calcular_prazo`, `buscar_legislacao`, `explicar_movimento`, `qual_conector_usar`.

### API
`GET /api/processo/{cnj}` · `GET /api/cnj/{cnj}` · `POST /api/prazo` · `GET /api/leis?q=` · `GET /api/traduzir?movimento=` · `GET /api/conectores`

## Limitações conhecidas
- Prazos: feriados nacionais + móveis embutidos; estaduais/municipais via `feriados_extras`. Não cobre prazos em dobro/Fazenda/MP nem contagem em horas.
- DataJud público tem dados parciais e atraso de atualização; scraping de e-SAJ não está incluído (use os conectores da camada 3).

Licença MIT.

## Deploy da API no Azure App Service (Linux)
```bash
az webapp up -n <app> -g <rg> -l brazilsouth --sku B1 --os-type Linux --runtime "PYTHON:3.12"
az webapp config appsettings set -g <rg> -n <app> --settings SCM_DO_BUILD_DURING_DEPLOYMENT=true DATAJUD_API_KEY=<chave>
az webapp config set -g <rg> -n <app> --startup-file "PYTHONPATH=src gunicorn -w 2 -k uvicorn.workers.UvicornWorker -b 0.0.0.0:8000 openjus.api:app"
az webapp config appsettings set -g <rg> -n <app> --settings WEBSITES_PORT=8000
```
Rode `az login` antes. Para o site no GitHub Pages falar com a API, hospede a API e aponte o front para ela (hoje o Pages só calcula prazos, no navegador).
