# OpenJus — revisão “só a busca”

Base: fonte original implantada no commit `690f788`.

## Escopo entregue

- Endpoint de leitura `GET /api/leis?q=...&limite=1..20&vigentes=false`.
- Indicador `indice_de_leis` em `GET /api/status`, consultando somente `LEIS_DB_PATH`.
- Corpus versionado, pequeno e verificável: CDC (Lei 8.078/1990), Código Civil (Lei 10.406/2002) e CPC (Lei 13.105/2015).
- Build determinístico local em `scripts/build_leis_iniciais.sh`: valida manifesto/SHA-256, sem clone e sem download, cria o índice FTS5 no build e valida 3 documentos.
- Separação obrigatória entre `LEIS_DB_PATH` (padrão `data/leis.db`) e `DB_PATH`; há recusa por mesmo caminho, symlink/hardlink e destino legado com tabelas que não são do índice.
- A busca pública existente passa a chamar `/api/leis` sem filtro de vigência: os três textos não têm uma afirmação de vigência atual verificável pelo serviço.

## Limites deliberados

Não é acervo completo, não certifica vigência atual e não substitui consulta à fonte oficial ou orientação jurídica. Metadados de fonte/versão/data do Markdown seguem no retorno quando declarados.

Nada nesta revisão altera memória, ensino, créditos, cotas, chatbot ou funcionalidades de marketing. A compatibilidade do banco mutável existente foi preservada; o novo endpoint e o build não o abrem para escrita.

## Publicação (requer aprovação posterior)

1. Revisar o patch e o ZIP; não há ação remota executada nesta preparação.
2. Aplicar o patch no repositório que está no commit-base `690f788`, incluindo os três textos, manifesto e licença.
3. Conferir no serviço Render que `LEIS_DB_PATH=data/leis.db` continua diferente de `DB_PATH`; não adicionar disco nem criar contas.
4. Autorizar e disparar um novo deploy. O `buildCommand` chama o script local e reconstrói o índice a cada build; o processo de runtime só lê esse artefato.
5. Após o deploy, validar `/api/saude`, `/api/status` (`indice_de_leis: true`) e `/api/leis?q=consumidor`.

No plano Free sem disco persistente, não há promessa de dados persistidos pelo usuário: o índice é recriado a partir do corpus versionado em cada redeploy.
