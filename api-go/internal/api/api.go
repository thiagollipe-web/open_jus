// Package api é o servidor HTTP do OpenJus em Go: os mesmos endpoints,
// caminhos e formatos de JSON do backend Python (FastAPI), para o
// site/index.html funcionar sem nenhuma mudança.
// Tradução direta de src/openjus/api.py.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thiagollipe-web/open_jus/api-go/internal/aprender"
	"github.com/thiagollipe-web/open_jus/api-go/internal/bot"
	"github.com/thiagollipe-web/open_jus/api-go/internal/cnj"
	"github.com/thiagollipe-web/open_jus/api-go/internal/config"
	"github.com/thiagollipe-web/open_jus/api-go/internal/cotas"
	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"github.com/thiagollipe-web/open_jus/api-go/internal/lingua"
	"github.com/thiagollipe-web/open_jus/api-go/internal/llm"
	"github.com/thiagollipe-web/open_jus/api-go/internal/referencias"
	"github.com/thiagollipe-web/open_jus/api-go/internal/servico"
)

// Versao é a versão da API (a mesma do FastAPI).
const Versao = "0.1.0"

// SitePublico é o destino do redirecionamento quando não há site/ local.
const SitePublico = "https://thiagollipe-web.github.io/open_jus/"

// Server é o servidor HTTP do OpenJus.
type Server struct {
	cfg      config.Settings
	lingua   *lingua.Lingua
	botLocal *bot.BotLocal
	botLLM   *bot.BotClaude
	servico  *servico.Servico
	limitador *cotas.Limiter
	mux      *http.ServeMux
}

// NewServer monta o servidor a partir da configuração.
func NewServer(cfg config.Settings) (*Server, error) {
	ling, err := carregarGlossario(cfg.GlossarioPath)
	if err != nil {
		return nil, fmt.Errorf("glossário: %w", err)
	}
	sv := &servico.Servico{
		LeisDBPath: cfg.LeisDBPath,
		MutableDB:  cfg.DBPath,
		DataJud:    datajud.NewClient(cfg.DataJudBaseURL, cfg.DataJudAPIKey),
		Lingua:     ling,
	}
	var freeLLM *llm.FreeLLM
	if cfg.FreeLLMBaseURL != "" || cfg.FreeLLMModel != "" {
		freeLLM = &llm.FreeLLM{BaseURL: cfg.FreeLLMBaseURL, APIKey: cfg.FreeLLMAPIKey, Model: cfg.FreeLLMModel}
	}
	var slm *llm.SLM
	if cfg.OllamaURL != "" {
		slm = &llm.SLM{URL: cfg.OllamaURL, Model: cfg.OllamaModel}
	}
	s := &Server{
		cfg:       cfg,
		lingua:    ling,
		servico:   sv,
		limitador: cotas.NewLimiter(),
		botLocal: &bot.BotLocal{
			LeisDBPath: cfg.LeisDBPath, MutableDB: cfg.DBPath, Lingua: ling,
			Servico: sv, FreeLLM: freeLLM, SLM: slm,
		},
		botLLM: &bot.BotClaude{
			Cliente: &llm.ClaudeClient{APIKey: cfg.AnthropicAPIKey, Model: cfg.AnthropicModel},
			Servico: sv, Lingua: ling,
		},
	}
	s.montarRotas()
	return s, nil
}

// carregarGlossario lê site/glossario.json quando existe; senão usa a
// cópia embutida no binário (o deploy no Render tem site/ no repo, mas o
// binário também funciona sem ele).
func carregarGlossario(path string) (*lingua.Lingua, error) {
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return lingua.CarregarGlossario(path)
		}
	}
	return lingua.CarregarGlossario("")
}

// Lingua expõe o glossário carregado (para testes).
func (s *Server) Lingua() *lingua.Lingua { return s.lingua }

// Handler devolve o handler HTTP completo (CORS + limite de taxa + rotas).
func (s *Server) Handler() http.Handler {
	return s.cors(s.limitarTaxa(s.mux))
}

func (s *Server) montarRotas() {
	m := http.NewServeMux()
	m.HandleFunc("/api/chat", s.chat)
	m.HandleFunc("/api/ensinar", s.ensinar)
	m.HandleFunc("/admin/pendentes", s.pendentes)
	m.HandleFunc("/admin/aprovar", s.aprovar)
	m.HandleFunc("/api/chat-llm", s.chatLLM)
	m.HandleFunc("/admin/chaves", s.novaChave)
	m.HandleFunc("/admin/creditos", s.creditar)
	m.HandleFunc("/api/status", s.status)
	m.HandleFunc("/api/saude", s.saude)
	m.HandleFunc("/api/leis", s.buscarLeis)
	m.HandleFunc("/api/processo/", s.processo)
	m.HandleFunc("/api/cnj/", s.cnj)
	m.HandleFunc("/api/prazo", s.prazo)
	m.HandleFunc("/api/traduzir", s.traduzir)
	m.HandleFunc("/api/conectores", s.conectores)
	if _, err := os.Stat(s.cfg.SiteDir); err == nil {
		m.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.cfg.SiteDir))))
	}
	m.HandleFunc("/", s.index)
	s.mux = m
}

// --- helpers HTTP ---

func escreverJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func erroJSON(w http.ResponseWriter, status int, msg string) {
	escreverJSON(w, status, map[string]string{"detail": msg})
}

// requireMethod responde 405 JSON quando o método não é o esperado.
func requireMethod(w http.ResponseWriter, r *http.Request, metodo string) bool {
	if r.Method != metodo {
		erroJSON(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return false
	}
	return true
}

func decodificarCorpo(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	// O pydantic ignora campos extras; aqui também.
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		erroJSON(w, http.StatusUnprocessableEntity, "Corpo JSON inválido.")
		return false
	}
	return true
}

func (s *Server) adminAutorizado(w http.ResponseWriter, r *http.Request) bool {
	esperado := s.cfg.AdminToken
	token := r.Header.Get("X-Admin-Token")
	if esperado == "" || subtle.ConstantTimeCompare([]byte(token), []byte(esperado)) != 1 {
		erroJSON(w, http.StatusForbidden, "Admin não autorizado.")
		return false
	}
	return true
}

// --- CORS (igual ao CORSMiddleware do FastAPI) ---

func (s *Server) cors(next http.Handler) http.Handler {
	permitidas := s.cfg.CORSAllowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origem := r.Header.Get("Origin")
		permitida := false
		for _, o := range permitidas {
			if o == origem {
				permitida = true
				break
			}
		}
		if origem != "" {
			w.Header().Add("Vary", "Origin")
		}
		if permitida {
			w.Header().Set("Access-Control-Allow-Origin", origem)
		}
		// Preflight.
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if !permitida {
				http.Error(w, "Disallowed CORS origin", http.StatusBadRequest)
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-OpenJus-Key")
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Limite de taxa (igual ao middleware do FastAPI) ---

func ipDoCliente(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) limitarTaxa(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/admin/") {
			if !s.limitador.Allow(ipDoCliente(r), s.cfg.RateLimitPerMin, time.Minute) {
				erroJSON(w, http.StatusTooManyRequests, "Muitas requisições. Tente em instantes.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// --- Handlers ---

type chatEntrada struct {
	Mensagens []map[string]interface{} `json:"mensagens"`
}

func conteudoMensagem(m map[string]interface{}) string {
	if c, ok := m["content"]; ok {
		switch v := c.(type) {
		case string:
			return v
		case nil:
			return ""
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// POST /api/chat — bot gratuito (sem LLM, sem chave): só limite por IP.
func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var entrada chatEntrada
	if !decodificarCorpo(w, r, &entrada) {
		return
	}
	if len(entrada.Mensagens) < 1 || len(entrada.Mensagens) > 40 {
		erroJSON(w, http.StatusUnprocessableEntity, "mensagens deve ter entre 1 e 40 itens.")
		return
	}
	ultimo := ""
	for i := len(entrada.Mensagens) - 1; i >= 0; i-- {
		if entrada.Mensagens[i]["role"] == "user" {
			ultimo = conteudoMensagem(entrada.Mensagens[i])
			break
		}
	}
	if len([]rune(ultimo)) > 1000 {
		erroJSON(w, 413, "Mensagem muito longa.")
		return
	}
	resp := s.botLocal.Conversar(r.Context(), ultimo)
	escreverJSON(w, http.StatusOK, resp)
}

type ensinoEntrada struct {
	Pergunta string `json:"pergunta"`
	Resposta string `json:"resposta"`
}

// POST /api/ensinar — recebe ensinamento; só vale após aprovação de admin.
func (s *Server) ensinar(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var e ensinoEntrada
	if !decodificarCorpo(w, r, &e) {
		return
	}
	if n := len([]rune(e.Pergunta)); n < 5 || n > 300 {
		erroJSON(w, http.StatusUnprocessableEntity, "pergunta deve ter entre 5 e 300 caracteres.")
		return
	}
	if n := len([]rune(e.Resposta)); n < 5 || n > 1500 {
		erroJSON(w, http.StatusUnprocessableEntity, "resposta deve ter entre 5 e 1500 caracteres.")
		return
	}
	if _, err := aprender.Ensinar(s.cfg.DBPath, e.Pergunta, e.Resposta); err != nil {
		erroJSON(w, http.StatusInternalServerError, "Não foi possível registrar o ensinamento.")
		return
	}
	escreverJSON(w, http.StatusOK, map[string]string{"status": "pendente", "mensagem": "Obrigado! Um moderador vai revisar."})
}

// GET /admin/pendentes
func (s *Server) pendentes(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.adminAutorizado(w, r) {
		return
	}
	pend, err := aprender.Pendentes(s.cfg.DBPath)
	if err != nil {
		erroJSON(w, http.StatusInternalServerError, "Não foi possível listar os pendentes.")
		return
	}
	lac, err := aprender.Lacunas(s.cfg.DBPath, 50)
	if err != nil {
		erroJSON(w, http.StatusInternalServerError, "Não foi possível listar as lacunas.")
		return
	}
	escreverJSON(w, http.StatusOK, map[string]interface{}{"pendentes": pend, "lacunas": lac})
}

type aprovarEntrada struct {
	ID int64 `json:"id"`
}

// POST /admin/aprovar
func (s *Server) aprovar(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.adminAutorizado(w, r) {
		return
	}
	var a aprovarEntrada
	if !decodificarCorpo(w, r, &a) {
		return
	}
	if a.ID == 0 {
		// O pydantic exige id; 0 não é um id válido de ensinamento.
		erroJSON(w, http.StatusUnprocessableEntity, "id é obrigatório.")
		return
	}
	ok, err := aprender.Aprovar(s.cfg.DBPath, a.ID)
	if err != nil {
		erroJSON(w, http.StatusInternalServerError, "Não foi possível aprovar o item.")
		return
	}
	if !ok {
		erroJSON(w, http.StatusNotFound, "Item não encontrado.")
		return
	}
	escreverJSON(w, http.StatusOK, map[string]string{"status": "aprovado"})
}

// POST /api/chat-llm — bot com LLM pago (Claude), com cotas.
func (s *Server) chatLLM(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var entrada chatEntrada
	if !decodificarCorpo(w, r, &entrada) {
		return
	}
	if len(entrada.Mensagens) < 1 || len(entrada.Mensagens) > 40 {
		erroJSON(w, http.StatusUnprocessableEntity, "mensagens deve ter entre 1 e 40 itens.")
		return
	}
	total := 0
	for _, m := range entrada.Mensagens {
		total += len([]rune(conteudoMensagem(m)))
	}
	if total > 8000 {
		erroJSON(w, 413, "Conversa muito longa. Comece uma nova.")
		return
	}
	chave := r.Header.Get("X-OpenJus-Key")
	var saldo *cotas.Saldo
	if chave != "" {
		var err error
		saldo, err = cotas.SaldoConta(s.cfg.DBPath, chave, s.cfg.FreeDailyTokens)
		if err != nil {
			erroJSON(w, http.StatusInternalServerError, "Não foi possível verificar a chave.")
			return
		}
	}
	if saldo == nil {
		erroJSON(w, http.StatusUnauthorized, "Chave de acesso ausente ou inválida (cabeçalho X-OpenJus-Key).")
		return
	}
	if saldo.GratisRestante <= 0 && saldo.Creditos <= 0 {
		erroJSON(w, 402, "Franquia grátis do dia esgotada e sem créditos. Adquira créditos.")
		return
	}
	mensagens := make([]llm.Mensagem, 0, len(entrada.Mensagens))
	for _, m := range entrada.Mensagens {
		role, _ := m["role"].(string)
		mensagens = append(mensagens, llm.Mensagem{Role: role, Content: conteudoMensagem(m)})
	}
	resp, err := s.botLLM.Conversar(r.Context(), mensagens)
	if err != nil {
		erroJSON(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	_ = cotas.Debitar(s.cfg.DBPath, chave, resp.Tokens, s.cfg.FreeDailyTokens)
	saldoAtual, _ := cotas.SaldoConta(s.cfg.DBPath, chave, s.cfg.FreeDailyTokens)
	escreverJSON(w, http.StatusOK, map[string]interface{}{
		"resposta": resp.Resposta, "ferramentas": resp.Ferramentas,
		"tokens": resp.Tokens, "saldo": saldoAtual,
	})
}

type novaChaveEntrada struct {
	Rotulo   string `json:"rotulo"`
	Creditos int    `json:"creditos"`
}

// POST /admin/chaves
func (s *Server) novaChave(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.adminAutorizado(w, r) {
		return
	}
	var n novaChaveEntrada
	if !decodificarCorpo(w, r, &n) {
		return
	}
	if n.Creditos < 0 {
		erroJSON(w, http.StatusUnprocessableEntity, "creditos deve ser maior ou igual a 0.")
		return
	}
	chave, err := cotas.CriarChave(s.cfg.DBPath, n.Rotulo, n.Creditos)
	if err != nil {
		erroJSON(w, http.StatusInternalServerError, "Não foi possível criar a chave.")
		return
	}
	escreverJSON(w, http.StatusOK, map[string]string{"chave": chave})
}

type creditoEntrada struct {
	Chave  string `json:"chave"`
	Tokens int    `json:"tokens"`
}

// POST /admin/creditos
func (s *Server) creditar(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.adminAutorizado(w, r) {
		return
	}
	var c creditoEntrada
	if !decodificarCorpo(w, r, &c) {
		return
	}
	if c.Chave == "" {
		erroJSON(w, http.StatusUnprocessableEntity, "chave é obrigatória.")
		return
	}
	if c.Tokens <= 0 {
		erroJSON(w, http.StatusUnprocessableEntity, "tokens deve ser maior que 0.")
		return
	}
	novo, err := cotas.AdicionarCreditos(s.cfg.DBPath, c.Chave, c.Tokens)
	if err != nil {
		erroJSON(w, http.StatusInternalServerError, "Não foi possível adicionar créditos.")
		return
	}
	if novo == nil {
		erroJSON(w, http.StatusNotFound, "Chave não encontrada.")
		return
	}
	escreverJSON(w, http.StatusOK, map[string]int{"creditos": *novo})
}

// GET /api/status — saúde genérica: não revela provedor, modelo nem chaves.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	escreverJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok", "versao": Versao,
		"indice_de_leis": leis.IndiceDisponivel(s.cfg.LeisDBPath),
	})
}

// GET /api/saude
func (s *Server) saude(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	escreverJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/leis — consulta somente leitura ao índice local de legislação.
func (s *Server) buscarLeis(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		erroJSON(w, http.StatusUnprocessableEntity, "Parâmetro 'q' é obrigatório.")
		return
	}
	limite := 5
	if v := r.URL.Query().Get("limite"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 20 {
			erroJSON(w, http.StatusUnprocessableEntity, "limite deve estar entre 1 e 20.")
			return
		}
		limite = n
	}
	vigentes := false
	if v := r.URL.Query().Get("vigentes"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			erroJSON(w, http.StatusUnprocessableEntity, "vigentes deve ser true ou false.")
			return
		}
		vigentes = b
	}
	if !leis.IndiceDisponivel(s.cfg.LeisDBPath) {
		erroJSON(w, http.StatusServiceUnavailable, "Índice de leis ausente ou vazio. Execute o build local antes de consultar.")
		return
	}
	trechos, err := leis.Buscar(s.cfg.LeisDBPath, q, limite, vigentes)
	if err != nil {
		var eIndice *leis.IndiceLeisErro
		if errors.As(err, &eIndice) {
			slog.Warn("consulta_leis_falhou", "erro", eIndice.Error())
			erroJSON(w, http.StatusServiceUnavailable, "Índice de leis temporariamente indisponível.")
			return
		}
		erroJSON(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	escreverJSON(w, http.StatusOK, trechos)
}

// GET /api/processo/{numero}
func (s *Server) processo(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	numero := strings.TrimPrefix(r.URL.Path, "/api/processo/")
	if numero == "" || strings.Contains(numero, "/") {
		erroJSON(w, http.StatusNotFound, "Not Found")
		return
	}
	res, err := s.servico.ConsultarProcesso(r.Context(), numero)
	if err != nil {
		var eCNJ *cnj.CNJError
		var eDJ *datajud.DataJudError
		switch {
		case errors.As(err, &eCNJ):
			erroJSON(w, http.StatusUnprocessableEntity, err.Error())
		case errors.As(err, &eDJ):
			erroJSON(w, http.StatusBadGateway, err.Error())
		default:
			erroJSON(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	escreverJSON(w, http.StatusOK, res)
}

// GET /api/cnj/{numero}
func (s *Server) cnj(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	numero := strings.TrimPrefix(r.URL.Path, "/api/cnj/")
	if numero == "" || strings.Contains(numero, "/") {
		erroJSON(w, http.StatusNotFound, "Not Found")
		return
	}
	n, err := cnj.ParseCNJ(numero)
	if err != nil {
		erroJSON(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	alias, err := n.AliasDataJud()
	if err != nil {
		erroJSON(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	escreverJSON(w, http.StatusOK, map[string]string{
		"formatado": n.Formatado(), "tribunal_datajud": alias,
	})
}

type prazoEntrada struct {
	Inicio         string   `json:"inicio"`
	Dias           int      `json:"dias"`
	DiasUteis      *bool    `json:"dias_uteis"`
	FeriadosExtras []string `json:"feriados_extras"`
}

// POST /api/prazo
func (s *Server) prazo(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var p prazoEntrada
	if !decodificarCorpo(w, r, &p) {
		return
	}
	inicio, err := time.Parse("2006-01-02", p.Inicio)
	if err != nil {
		erroJSON(w, http.StatusUnprocessableEntity, "inicio deve ser uma data AAAA-MM-DD.")
		return
	}
	if p.Dias < 1 || p.Dias > 365 {
		erroJSON(w, http.StatusUnprocessableEntity, "dias deve estar entre 1 e 365.")
		return
	}
	diasUteis := true
	if p.DiasUteis != nil {
		diasUteis = *p.DiasUteis
	}
	var extras []time.Time
	for _, d := range p.FeriadosExtras {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			erroJSON(w, http.StatusUnprocessableEntity, "feriados_extras deve conter datas AAAA-MM-DD.")
			return
		}
		extras = append(extras, t)
	}
	res, err := servico.Prazo(inicio, p.Dias, extras, diasUteis)
	if err != nil {
		erroJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	escreverJSON(w, http.StatusOK, res)
}

// GET /api/traduzir
func (s *Server) traduzir(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	movimento := r.URL.Query().Get("movimento")
	if movimento == "" {
		erroJSON(w, http.StatusUnprocessableEntity, "Parâmetro 'movimento' é obrigatório.")
		return
	}
	escreverJSON(w, http.StatusOK, map[string]string{
		"explicacao": lingua.RemoverCliches(s.lingua.TraduzirMovimento(movimento)),
	})
}

// GET /api/conectores
func (s *Server) conectores(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	out := map[string]referencias.Conector{}
	for k, v := range referencias.Conectores {
		out[k] = v
	}
	escreverJSON(w, http.StatusOK, out)
}

// GET / — serve o site local ou redireciona para o site público.
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	indice := filepath.Join(s.cfg.SiteDir, "index.html")
	if _, err := os.Stat(indice); err == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFile(w, r, indice)
		return
	}
	http.Redirect(w, r, SitePublico, http.StatusTemporaryRedirect)
}

// Servir sobe o servidor HTTP (bloqueante).
func (s *Server) Servir(ctx context.Context) error {
	endereco := ":" + s.cfg.Port
	srv := &http.Server{Addr: endereco, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("openjus_api", "porta", s.cfg.Port, "indice_de_leis", leis.IndiceDisponivel(s.cfg.LeisDBPath))
	return srv.ListenAndServe()
}
