// Package bot implementa os dois bots do OpenJus:
//   - BotLocal: por regras, sem LLM e sem chave (bot_local.py).
//   - BotClaude: com LLM pago (Claude + tool use) (bot.py).
// O LLM nunca calcula prazo nem cita lei de memória: usa as ferramentas
// determinísticas calcular_prazo e buscar_legislacao.
package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thiagollipe-web/open_jus/api-go/internal/aprender"
	"github.com/thiagollipe-web/open_jus/api-go/internal/cnj"
	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
	"github.com/thiagollipe-web/open_jus/api-go/internal/llm"
	"github.com/thiagollipe-web/open_jus/api-go/internal/servico"
)

// Ajuda é a mensagem de ajuda do bot local.
const Ajuda = "Posso ajudar com:\n" +
	"• Prazo: \"intimado em 07/03/2025, prazo de 15 dias úteis\"\n" +
	"• Processo: envie o número CNJ (NNNNNNN-DD.AAAA.J.TT.OOOO)\n" +
	"• Termos: \"o que significa trânsito em julgado?\"\n" +
	"• Lei: \"o que diz a lei sobre prazo de contestação?\"\n" +
	"Informativo; não substitui advogado."

// PedeCNJ é a resposta quando o usuário fala de processo sem o número.
const PedeCNJ = "Para consultar um processo, envie o número CNJ completo, no formato " +
	"NNNNNNN-DD.AAAA.J.TT.OOOO (ex.: 0000000-00.2024.8.26.0100). Ele aparece na " +
	"intimação, no site do tribunal ou com seu advogado."

var (
	cnjRE      = regexp.MustCompile(`\b\d{7}-?\d{2}\.?\d{4}\.?\d\.?\d{2}\.?\d{4}\b`)
	dataRE     = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{4})\b|\b(\d{4})-(\d{2})-(\d{2})\b`)
	diasRE     = regexp.MustCompile(`(?i)\b(\d{1,3})\s*dias?\b`)
	prazoRE    = regexp.MustCompile(`(?i)\b(prazo|vence|vencimento|contestar|recorrer)\b`)
	processoRE = regexp.MustCompile(`(?i)\bprocess(o|os|ual)\b`)
	saudacaoRE = regexp.MustCompile(`(?i)^\s*(oi|ol[aá]|bom dia|boa tarde|boa noite|e a[ií])[!.\s]*$`)
	// O \b do Go é ASCII-only e falha após "olá" (á não é word char em
	// RE2): bordas explícitas Unicode — para um teste booleano, o consumo
	// da borda não importa.
	ajudaRE = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_])(ajuda|oi|olá|ola|menu)(?:[^\p{L}\p{N}_]|$)`)
	corridosRE = regexp.MustCompile(`(?i)corrid`)
)

// Resposta é o retorno dos bots: texto, ferramentas usadas e tokens.
type Resposta struct {
	Resposta    string   `json:"resposta"`
	Ferramentas []string `json:"ferramentas"`
	Tokens      int      `json:"tokens"`
}

// BotLocal é o bot por regras.
type BotLocal struct {
	LeisDBPath   string // índice de leis (leis.db)
	MutableDB    string // estado mutável (openjus.db): aprendizado e lacunas
	Lingua       *lingua.Lingua
	Servico      *servico.Servico
	FreeLLM      *llm.FreeLLM
	SLM          *llm.SLM
}

// data extrai uma data dd/mm/aaaa ou aaaa-mm-dd do texto.
func extrairData(texto string) (time.Time, bool) {
	m := dataRE.FindStringSubmatch(texto)
	if m == nil {
		return time.Time{}, false
	}
	var ano, mes, dia int
	if m[1] != "" {
		dia, _ = strconv.Atoi(m[1])
		mes, _ = strconv.Atoi(m[2])
		ano, _ = strconv.Atoi(m[3])
	} else {
		ano, _ = strconv.Atoi(m[4])
		mes, _ = strconv.Atoi(m[5])
		dia, _ = strconv.Atoi(m[6])
	}
	t := time.Date(ano, time.Month(mes), dia, 0, 0, 0, 0, time.UTC)
	if t.Year() != ano || int(t.Month()) != mes || t.Day() != dia {
		return time.Time{}, false
	}
	return t, true
}

func br(iso string) string {
	p := strings.Split(iso, "-")
	if len(p) != 3 {
		return iso
	}
	return p[2] + "/" + p[1] + "/" + p[0]
}

func fatiarRunas(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// prazo calcula o prazo a partir do texto (data + nº de dias).
func (b *BotLocal) prazo(texto string) Resposta {
	ini, okData := extrairData(texto)
	mDias := diasRE.FindStringSubmatch(texto)
	if !okData || mDias == nil {
		return Resposta{Resposta: "Para calcular o prazo preciso da data da intimação/publicação e do " +
			"número de dias. Ex.: \"intimado em 07/03/2025, 15 dias úteis\".", Ferramentas: []string{}}
	}
	dias, _ := strconv.Atoi(mDias[1])
	corridos := corridosRE.MatchString(texto)
	r, err := servico.Prazo(ini, dias, nil, !corridos)
	if err != nil {
		// O Python deixava o ValueError subir (500); o bot Go responde
		// com a mensagem em vez de quebrar a conversa.
		return Resposta{Resposta: err.Error(), Ferramentas: []string{}}
	}
	tipo := "dias úteis"
	if corridos {
		tipo = "dias corridos"
	}
	return Resposta{
		Resposta: fmt.Sprintf("Prazo de %d %s a partir de %s:\n• Começa a contar em %s\n• Vence em %s\nBase: CPC arts. 219 (dias úteis), 220 (recesso 20/12–20/01) e 224. %s",
			r.Dias, tipo, br(r.InicioEvento), br(r.InicioContagem), br(r.Vencimento), r.Aviso),
		Ferramentas: []string{"calcular_prazo"},
	}
}

// processo consulta um processo pelo número CNJ.
func (b *BotLocal) processo(ctx context.Context, numero string) Resposta {
	r, err := b.Servico.ConsultarProcesso(ctx, numero)
	if err != nil {
		var eCNJ *cnj.CNJError
		var eDJ *datajud.DataJudError
		switch {
		case errors.As(err, &eCNJ):
			return Resposta{Resposta: "Número CNJ inválido: " + err.Error(), Ferramentas: []string{}}
		case errors.As(err, &eDJ):
			return Resposta{Resposta: "Não consegui consultar o DataJud agora: " + err.Error(), Ferramentas: []string{}}
		default:
			return Resposta{Resposta: "Não consegui consultar o DataJud agora: " + err.Error(), Ferramentas: []string{}}
		}
	}
	if !r.Encontrado {
		return Resposta{Resposta: "Processo não encontrado no DataJud.", Ferramentas: []string{"consultar_processo"}}
	}
	if r.Sigiloso {
		return Resposta{Resposta: r.Aviso, Ferramentas: []string{"consultar_processo"}}
	}
	assuntos := strings.Join(r.Assuntos, ", ")
	if assuntos == "" {
		assuntos = "—"
	}
	linhas := []string{
		"Classe: " + r.Classe,
		"Assuntos: " + assuntos,
		"Órgão: " + r.OrgaoJulgador,
		"Últimas movimentações:",
	}
	movs := r.Movimentos
	if len(movs) > 5 {
		movs = movs[:5]
	}
	for _, m := range movs {
		data := m.Data
		if len(data) > 10 {
			data = data[:10]
		}
		linhas = append(linhas, fmt.Sprintf("• %s — %s: %s", data, m.Nome, m.Explicacao))
	}
	return Resposta{Resposta: strings.Join(linhas, "\n"), Ferramentas: []string{"consultar_processo"}}
}

// lei busca trechos no índice de leis. Diferente do Python — que lia o
// banco mutável (sempre vazio) e nunca achava nada — o Go busca no índice
// real de leis (LEIS_DB_PATH). Veja o relatório.
func (b *BotLocal) lei(ctx context.Context, texto string) (Resposta, []string) {
	if _, err := os.Stat(b.LeisDBPath); err != nil || !leis.IndiceDisponivel(b.LeisDBPath) {
		return Resposta{Resposta: "Ainda não sei responder isso com segurança. Tente reformular, ou me " +
			"ensine com `ensinar: pergunta => resposta`.\n\n" + Ajuda, Ferramentas: []string{}}, nil
	}
	achados, err := leis.Buscar(b.LeisDBPath, texto, 3, true)
	if err != nil || len(achados) == 0 {
		_ = aprender.RegistrarLacuna(b.MutableDB, texto) // vira pauta para alguém ensinar
		return Resposta{Resposta: "Ainda não sei responder isso. Você pode me ensinar com " +
			"`ensinar: pergunta => resposta` (um moderador revisa antes de eu usar).\n\n" + Ajuda,
			Ferramentas: []string{"buscar_legislacao"}}, nil
	}
	var partes []string
	var trechos []string
	for _, t := range achados {
		urn := ""
		if t.Urn != "" {
			urn = " (" + t.Urn + ")"
		}
		partes = append(partes, fmt.Sprintf("• %s%s:\n  %s…", t.Titulo, urn, strings.TrimSpace(fatiarRunas(t.Texto, 400))))
		trechos = append(trechos, fatiarRunas(t.Texto, 600))
	}
	return Resposta{Resposta: "Trechos encontrados (confira o texto oficial):\n" + strings.Join(partes, "\n"),
		Ferramentas: []string{"buscar_legislacao"}}, trechos
}

// Conversar responde a uma mensagem pelo bot local (sem LLM).
func (b *BotLocal) Conversar(ctx context.Context, texto string) Resposta {
	texto = strings.TrimSpace(texto)
	if texto == "" {
		return Resposta{Resposta: Ajuda, Ferramentas: []string{}, Tokens: 0}
	}
	if m := cnjRE.FindString(texto); m != "" {
		return b.processo(ctx, m)
	}
	if _, okData := extrairData(texto); prazoRE.MatchString(texto) && (okData || diasRE.MatchString(texto)) {
		return b.prazo(texto)
	}
	if processoRE.MatchString(texto) && len([]rune(texto)) < 80 {
		return Resposta{Resposta: PedeCNJ, Ferramentas: []string{}}
	}
	if saudacaoRE.MatchString(texto) {
		return Resposta{Resposta: "Olá! " + Ajuda, Ferramentas: []string{}}
	}
	if termo := b.Lingua.TermoContido(texto); termo != "" {
		return Resposta{
			Resposta: fmt.Sprintf("%s: %s", lingua.Capitalizar(termo), b.Lingua.TraduzirMovimento(termo)),
			Ferramentas: []string{"explicar_movimento"},
		}
	}
	if b.MutableDB != "" {
		// O Python só busca aprendizado se o banco mutável existir.
		if _, err := os.Stat(b.MutableDB); err == nil {
			if a, err := aprender.Buscar(b.MutableDB, texto, 0.6); err == nil && a != "" {
				return Resposta{Resposta: a + "\n(resposta aprendida e aprovada por moderador)", Ferramentas: []string{"aprendizado"}}
			}
		}
	}
	if ajudaRE.MatchString(texto) && len([]rune(texto)) < 20 {
		return Resposta{Resposta: Ajuda, Ferramentas: []string{}}
	}
	out, trechos := b.lei(ctx, texto)
	if b.FreeLLM != nil && b.FreeLLM.Configurado() {
		if ia := b.FreeLLM.Perguntar(ctx, texto, trechos); ia != "" {
			if len(trechos) > 0 {
				out.Resposta = ia + "\n(resumo por IA; confira os trechos oficiais)\n\n" + out.Resposta
				out.Ferramentas = append(out.Ferramentas, "ia")
			} else {
				out = Resposta{Resposta: ia + "\n\n⚠️ Resposta geral de IA, sem fonte legal anexada. " +
					"Confirme com um advogado ou a Defensoria Pública.", Ferramentas: []string{"ia"}}
			}
		}
	} else if b.SLM != nil {
		if resumo := b.SLM.Resumir(ctx, texto, trechos); resumo != "" {
			out.Resposta = resumo + "\n(resumo por IA local; confira os trechos oficiais)\n\n" + out.Resposta
			out.Ferramentas = append(out.Ferramentas, "slm")
		}
	}
	out.Tokens = 0
	return out
}

// --- Bot com LLM (Claude) ---

// BotClaude é o bot opcional com LLM pago (Claude + tool use).
type BotClaude struct {
	Cliente *llm.ClaudeClient
	Servico *servico.Servico
	Lingua  *lingua.Lingua
}

// executarFerramenta executa uma ferramenta do Claude. Erros voltam ao
// modelo para ele se explicar (como no Python).
func (b *BotClaude) executarFerramenta(ctx context.Context, nome string, args map[string]interface{}) interface{} {
	defer func() {
		_ = recover()
	}()
	switch nome {
	case "calcular_prazo":
		dataInicio, _ := args["data_inicio"].(string)
		diasF, _ := args["dias"].(float64)
		diasUteis := true
		if v, ok := args["dias_uteis"].(bool); ok {
			diasUteis = v
		}
		var extras []time.Time
		if arr, ok := args["feriados_extras"].([]interface{}); ok {
			for _, d := range arr {
				if s, ok := d.(string); ok {
					if t, err := time.Parse("2006-01-02", s); err == nil {
						extras = append(extras, t)
					}
				}
			}
		}
		t, err := time.Parse("2006-01-02", dataInicio)
		if err != nil {
			return map[string]string{"erro": fmt.Sprintf("time.ParseError: %v", err)}
		}
		m, err := servico.PrazoMapa(t, int(diasF), extras, diasUteis)
		if err != nil {
			return map[string]string{"erro": "ValueError: " + err.Error()}
		}
		return m
	case "buscar_legislacao":
		consulta, _ := args["consulta"].(string)
		limite := 5
		if v, ok := args["limite"].(float64); ok {
			limite = int(v)
		}
		// Divergência documentada do Python: o Python lia o banco mutável
		// (sempre vazio) aqui; o Go busca no índice real de leis.
		if _, err := os.Stat(b.Servico.LeisDBPath); err != nil {
			return map[string]string{"erro": "Índice de leis ausente (rode openjus-ingest)."}
		}
		trechos, err := leis.Buscar(b.Servico.LeisDBPath, consulta, limite, true)
		if err != nil {
			return map[string]string{"erro": fmt.Sprintf("%T: %v", err, err)}
		}
		// O Python devolve t.__dict__ (inclui score) ao modelo.
		out := make([]map[string]interface{}, 0, len(trechos))
		for _, t := range trechos {
			raw, _ := json.Marshal(t)
			var m map[string]interface{}
			_ = json.Unmarshal(raw, &m)
			m["score"] = t.Score()
			out = append(out, m)
		}
		return out
	case "consultar_processo":
		numero, _ := args["numero_cnj"].(string)
		r, err := b.Servico.ConsultarProcesso(ctx, numero)
		if err != nil {
			return map[string]string{"erro": fmt.Sprintf("%T: %v", err, err)}
		}
		return r
	case "explicar_movimento":
		nome, _ := args["nome_movimento"].(string)
		return b.Lingua.TraduzirMovimento(nome)
	default:
		return map[string]string{"erro": "ferramenta desconhecida: " + nome}
	}
}

// Conversar roda o loop de tool use do Claude (no máximo 6 passos).
func (b *BotClaude) Conversar(ctx context.Context, mensagens []llm.Mensagem) (Resposta, error) {
	if b.Cliente == nil || b.Cliente.APIKey == "" {
		return Resposta{}, fmt.Errorf("ANTHROPIC_API_KEY não configurada (veja .env.example).")
	}
	msgs := append([]llm.Mensagem{}, mensagens...)
	usadas := []string{}
	tokens := 0
	for range 6 {
		r, err := b.Cliente.CriarMensagem(ctx, llm.SistemaClaude(time.Now()), llm.FerramentasClaude, msgs, 1500)
		if err != nil {
			return Resposta{}, err
		}
		tokens += r.Tokens
		if r.StopReason != "tool_use" {
			var texto strings.Builder
			for _, bloco := range r.Content {
				if bloco.Type == "text" {
					texto.WriteString(bloco.Text)
				}
			}
			return Resposta{Resposta: lingua.RemoverCliches(texto.String()), Ferramentas: usadas, Tokens: tokens}, nil
		}
		msgs = append(msgs, llm.Mensagem{Role: "assistant", Content: r.Content})
		var resultados []llm.Bloco
		for _, bloco := range r.Content {
			if bloco.Type != "tool_use" {
				continue
			}
			usadas = append(usadas, bloco.Name)
			saida := b.executarFerramenta(ctx, bloco.Name, bloco.Input)
			conteudo, _ := json.Marshal(saida)
			resultados = append(resultados, llm.Bloco{
				Type: "tool_result", ToolUseID: bloco.ID, Content: string(conteudo),
			})
		}
		msgs = append(msgs, llm.Mensagem{Role: "user", Content: resultados})
	}
	return Resposta{Resposta: "Não consegui concluir. Reformule a pergunta.", Ferramentas: usadas, Tokens: tokens}, nil
}
