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
| 🧠 | **Bot gratuito** | Responde prazos, processos, termos e leis por regras, sem LLM e sem API paga (`POST /api/chat`) |
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

## 🆓 Bot gratuito (sem LLM, sem API paga)

`POST /api/chat` **não usa Claude, Azure nem qualquer API paga**. Ele interpreta a pergunta por regras e chama as ferramentas locais:

| Você escreve | O bot faz |
|---|---|
| "intimado em 07/03/2025, prazo de 15 dias úteis" | calcula o prazo (CPC 219/220/224, calendário offline) |
| número CNJ | consulta a **API pública e gratuita do DataJud** e explica as movimentações |
| "o que significa trânsito em julgado?" | usa o glossário offline |
| pergunta sobre lei | busca no índice local do `ctresb/leis` e cita título/URN |

### 🧠 Aprendizado recorrente (sem LLM)
- **No navegador:** o bot roda em JavaScript (prazos, glossário, aprendizado), então **funciona no GitHub Pages sem servidor**. Ensine com `ensinar: pergunta => resposta`; fica salvo no seu navegador (`esquecer tudo` apaga).
- **No servidor:** o ensinamento vai para `POST /api/ensinar` como **pendente**. Um admin revisa (`GET /admin/pendentes`) e aprova (`POST /admin/aprovar`); só então o bot passa a responder aquilo para todos, por similaridade de texto. Perguntas sem resposta viram **lacunas** (ordenadas por frequência) para você saber o que ensinar.
- A moderação evita que alguém envenene o bot com informação jurídica errada.

### 🪶 SLM leve opcional (grátis, sem API paga)
Um modelo pequeno **só reescreve em linguagem simples o que as fontes já trouxeram**; sem contexto recuperado, ele nem é chamado (modelo pequeno inventa lei, então a regra vem antes).

- **No navegador:** botão **"Ativar IA local"** no site. Carrega `onnx-community/Qwen2.5-0.5B-Instruct` (Qwen 2.5, 0,5 B, ≈400 MB, quantizado) via [Transformers.js](https://github.com/huggingface/transformers.js), com WebGPU quando disponível, senão WASM. Baixa uma vez e fica em cache.
- **No servidor:** defina `OLLAMA_URL=http://localhost:11434` e rode `ollama pull qwen2.5:0.5b`. O resumo é anexado às respostas de busca em lei.

Só há limite por IP (`RATE_LIMIT_PER_MIN`, padrão 60 → 429). Não entende linguagem livre complexa: fora desses casos, mostra a ajuda.

### 🤖 IA gratuita (opcional, no servidor)
Funciona com **qualquer API compatível com OpenAI** que tenha plano grátis (Groq, OpenRouter modelos `:free`, Gemini no endpoint compatível, ou um agregador como o freellmapi). A chave fica **só no servidor**, nunca no site.

```env
FREE_LLM_BASE_URL=https://api.groq.com/openai/v1   # exemplo
FREE_LLM_API_KEY=sua_chave
FREE_LLM_MODEL=nome-do-modelo
```
Com trechos de lei encontrados, a IA resume ancorada neles. Sem trechos, dá só orientação geral, marcada como **sem fonte**. Se o plano grátis estourar o limite, o bot volta às regras.

### 🚀 Deploy grátis no Render
1. Em [render.com](https://render.com): **New + → Blueprint** → escolha este repositório (usa `render.yaml`).
2. Preencha `DATAJUD_API_KEY` e as variáveis `FREE_LLM_*`.
3. Copie a URL gerada (ex.: `https://openjus-api.onrender.com`) e coloque em `site/index.html`, na tag `<meta name="openjus-api">`.

No plano grátis o serviço "dorme" após inatividade (a 1ª resposta demora) e o disco é temporário (ensinamentos e histórico somem a cada reinício).

### Opcional: bot com LLM pago
`POST /api/chat-llm` (Claude) é opcional: `pip install openjus[llm]` + `ANTHROPIC_API_KEY`. Exige chave de acesso por usuário (`X-OpenJus-Key`, emitida em `POST /admin/chaves`), tem franquia grátis diária (`FREE_DAILY_TOKENS`) e créditos pré-pagos (`POST /admin/creditos`); sem saldo → 402.

## ⚠️ Limitações

- Feriados estaduais e municipais entram via `feriados_extras`.
- Sem prazo em dobro (Fazenda, MP) nem contagem em horas.
- O DataJud público tem dados parciais e atraso de atualização.
- Não há scraping de e-SAJ; use os conectores da camada 3.

## 🤝 Contribuindo

Issues e PRs são bem-vindos. Antes de abrir um PR: `uv run ruff check src tests && uv run pytest`.

## 💛 Apoie o projeto

O OpenJus é gratuito e mantido no tempo livre. Se ele te ajudou e você quiser contribuir, aceito doações via **Pix**:

**Pix Copia e Cola** (titular: Thiago Fillipe Soares):

```
00020126360014br.gov.bcb.pix0114+55119864553905204000053039865802BR5921Thiago Fillipe Soares6009Sao Paulo62230519daqr25678757257972763047D53
```

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
