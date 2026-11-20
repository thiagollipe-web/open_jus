<div align="center">

# ⚖️ OpenJus

**Bot jurídico open source para o Direito brasileiro.**
Legislação, processos do DataJud/CNJ e prazos do CPC — numa API e num site.

🌐 **Site:** https://thiagollipe-web.github.io/open_jus — grátis, sem cadastro e sem login.

[![CI](https://github.com/thiagollipe-web/open_jus/actions/workflows/ci.yml/badge.svg)](https://github.com/thiagollipe-web/open_jus/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/go-1.26%2B-blue)
![Licença](https://img.shields.io/badge/licen%C3%A7a-MIT-green)

</div>

> ⚠️ **Aviso:** ferramenta informativa. Não substitui advogado. Confira feriados locais e a data efetiva da intimação.

> ℹ️ **Backend em Go:** a API foi reescrita de Python (FastAPI) para Go, com o mesmo contrato HTTP — o site não mudou. O servidor MCP (`openjus-mcp`, comando `api-go/cmd/mcp`) também foi reescrito em Go, com as mesmas 5 tools do Python.

## ✨ O que ele faz

| | Recurso | Detalhe |
|---|---|---|
| 📚 | **Busca em legislação** | Busca nas 6 leis federais indexadas (CDC, Estatuto da Advocacia, Juizados Especiais, Código Civil, CPC e LGPD) e cita URN, situação e arquivo de origem. Não é acervo completo — a lista está no site, na seção Acervo |
| 📂 | **Consulta de processos** | Número CNJ validado (dígito verificador) → DataJud → classe, assuntos, órgão e movimentações |
| 🗣️ | **Juridiquês → português** | Traduz movimentações e remove clichês de IA |
| ⏱️ | **Prazos do CPC** | Arts. 219, 220 e 224, com calendário forense offline |
| 🔒 | **LGPD** | Segredo de justiça não é gravado; CPFs são mascarados (Res. CNJ 331/2020) |
| 🧠 | **Bot gratuito** | Responde prazos, processos, termos e leis por regras, sem LLM e sem API paga (`POST /api/chat`) |

## 🧱 Arquitetura em 4 camadas

```
 Usuário / Agente de IA
        │
 ┌──────▼───────┐   API Go · Site
 │ 4 Linguagem  │   tradução de movimentos · prazos (CPC)
 ├──────────────┤
 │ 3 Referências│   qual conector do ecossistema usar
 ├──────────────┤
 │ 2 Memória    │   snapshots SQLite · LGPD
 ├──────────────┤
 │ 1 Contexto   │   ctresb/leis (FTS5) · DataJud · número CNJ
 └──────────────┘
```

| Camada | Pacote Go | Função |
|---|---|---|
| 1. Contexto | `internal/leis`, `internal/datajud`, `internal/cnj` | Ingestão da legislação e consulta de processos |
| 2. Memória | `internal/memoria` | Histórico de andamentos e detecção de movimentos novos |
| 3. Referências | `internal/referencias` | Aponta o repositório certo: DataJUD_API_CALLER (lote), api_cnj (pandas), mcp-juridico-brasil (agente), jjesusfilho/tjsp e crawler-jus (e-SAJ/scraping) |
| 4. Linguagem | `internal/lingua`, `internal/prazos` | Tradução de juridiquês e cálculo de prazos |

**Como cruza leis × processo:** o DataJud devolve classe e assuntos; o OpenJus monta uma busca na legislação *vigente* (campo `situacao` do YAML) e devolve os trechos com URN para citação.

## 🚀 Início rápido

```bash
git clone https://github.com/thiagollipe-web/open_jus && cd open_jus
cp .env.example .env                           # preencha DATAJUD_API_KEY
sh scripts/build_leis_iniciais.sh              # indexa a legislação (6 leis)
go build -C api-go -o openjus-api ./cmd/api    # compila o servidor (binário em api-go/)
./api-go/openjus-api                           # site + API em http://localhost:8000
go test -C api-go ./...                        # testes
go vet -C api-go ./...                         # verificação estática
```

A chave do DataJud é gratuita: veja a [wiki do CNJ](https://datajud-wiki.cnj.jus.br/api-publica/acesso).

O binário é Go puro (SQLite via `modernc.org/sqlite`, sem cgo): um único arquivo, sem dependência de runtime.

### 🌐 API

| Método | Rota | Descrição |
|---|---|---|
| GET | `/api/processo/{cnj}` | Consulta o processo e cruza com a legislação |
| GET | `/api/cnj/{cnj}` | Valida o número e mostra o tribunal |
| POST | `/api/prazo` | Calcula prazo (`inicio`, `dias`, `dias_uteis`, `feriados_extras`) |
| GET | `/api/leis?q=` | Busca na legislação (`limite` 1–20, `vigentes`) |
| GET | `/api/traduzir?movimento=` | Explica uma movimentação |
| GET | `/api/conectores` | Lista os conectores do ecossistema |
| GET | `/api/status` | Saúde genérica (não revela provedor, modelo nem chaves) |
| GET | `/api/saude` | Saúde simples (`{"status": "ok"}`) |
| POST | `/api/chat` | Bot gratuito por regras (sem chave) |
| POST | `/api/ensinar` | Envia ensinamento (fica pendente de moderação) |
| POST | `/api/chat-llm` | Bot com Claude (exige `X-OpenJus-Key`) |
| GET | `/admin/pendentes` | Ensinamentos pendentes e lacunas (exige `X-Admin-Token`) |
| POST | `/admin/aprovar` | Aprova um ensinamento |
| POST | `/admin/chaves` | Emite uma chave de acesso |
| POST | `/admin/creditos` | Adiciona créditos a uma chave |

## 🆓 Bot gratuito (sem LLM, sem API paga)

`POST /api/chat` **não usa Claude, Azure nem qualquer API paga**. Ele interpreta a pergunta por regras e chama as ferramentas locais:

| Você escreve | O bot faz |
|---|---|
| "intimado em 07/03/2025, prazo de 15 dias úteis" | calcula o prazo (CPC 219/220/224, calendário offline) |
| número CNJ | consulta a **API pública e gratuita do DataJud** e explica as movimentações |
| "o que significa trânsito em julgado?" | usa o glossário offline |
| pergunta sobre lei | busca no índice local das 6 leis indexadas e cita título/URN |

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
`POST /api/chat-llm` (Claude) é opcional: basta `ANTHROPIC_API_KEY` (o cliente Claude é HTTP puro, sem SDK). Exige chave de acesso por usuário (`X-OpenJus-Key`, emitida em `POST /admin/chaves`), tem franquia grátis diária (`FREE_DAILY_TOKENS`) e créditos pré-pagos (`POST /admin/creditos`); sem saldo → 402.

## 📚 Acervo de leis (o que a busca cobre hoje)

A busca cobre **6 leis federais**, com textos do repositório público [`ctresb/leis`](https://github.com/ctresb/leis) (commit `c9f21b4a`), conferidos por SHA-256 no build:

| Lei | Assunto |
|---|---|
| Lei nº 8.078/1990 — CDC | Defesa do consumidor |
| Lei nº 8.906/1994 — Estatuto da Advocacia | Advocacia e OAB |
| Lei nº 9.099/1995 — Juizados Especiais | Causas de menor complexidade |
| Lei nº 10.406/2002 — Código Civil | Contratos, obrigações, família e sucessões |
| Lei nº 13.105/2015 — CPC | Processo civil e prazos |
| Lei nº 13.709/2018 — LGPD | Proteção de dados pessoais |

**Não é acervo completo e não certifica vigência.** Para ampliar: baixe o texto da mesma fonte (mesmo commit), coloque em `data/leis_iniciais/federal/leis-ordinarias/<ano>/`, registre `bytes` e `sha256` em `data/FONTE_LEIS.json` e rode `sh scripts/build_leis_iniciais.sh` — o indexador recusa qualquer arquivo fora do manifesto.

## 📊 Medição (privacidade)

O site usa **Plausible** (sem cookies, sem dados pessoais, sem rastreamento entre sites) para saber quantas visitas chegam e quais ferramentas são usadas — nada é vendido e ninguém é identificado. O script já está em `site/index.html` (`data-domain="thiagollipe-web.github.io"`), mas **só passa a registrar depois de ativar**:

1. Crie uma conta em [plausible.io](https://plausible.io) (há teste grátis; também dá para auto-hospedar).
2. Adicione o site com o domínio `thiagollipe-web.github.io`.
3. Pronto: as visitas começam a aparecer no painel. Sem esse cadastro, o script não coleta nada — o site funciona igual.

## ⚠️ Limitações

- Feriados estaduais e municipais entram via `feriados_extras`.
- Sem prazo em dobro (Fazenda, MP) nem contagem em horas.
- O DataJud público tem dados parciais e atraso de atualização.
- Não há scraping de e-SAJ; use os conectores da camada 3.

## 🤝 Contribuindo

Issues e PRs são bem-vindos. Antes de abrir um PR: `go vet -C api-go ./... && go test -C api-go ./...`.

## 💛 Apoie o projeto

O OpenJus é gratuito e mantido no tempo livre. Se ele te ajudou e você quiser contribuir, aceito doações via **Pix**: escaneie o QR Code na seção Apoie do site ou use o botão **Copiar Pix** por lá. O código não fica escrito no README por segurança.

Ajudar também vale: dê uma ⭐ no repositório, divulgue e envie sugestões.

## 📄 Licença

[MIT](LICENSE)
