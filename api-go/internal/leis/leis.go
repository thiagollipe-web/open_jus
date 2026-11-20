// Package leis é a camada 1 (Legislação): índice SQLite FTS5 isolado em
// tabelas leis_*, com busca por BM25 + re-ranking por cobertura da
// consulta (nome da lei e frase exata pesam mais que o BM25).
// Tradução direta de src/openjus/leis.py. O índice é separado do banco
// mutável de memória, cotas e aprendizado.
package leis

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

// IndiceLeisErro é um erro controlado de índice/consulta de legislação.
type IndiceLeisErro struct{ msg string }

func (e *IndiceLeisErro) Error() string { return e.msg }

func indiceErr(format string, args ...any) *IndiceLeisErro {
	return &IndiceLeisErro{msg: fmt.Sprintf(format, args...)}
}

var (
	frontRE   = regexp.MustCompile(`(?s)\A---\s*\n(.*?)\n---\s*\n`)
	artigoRE  = regexp.MustCompile(`(?m)^\s*(?:#+\s*)?\**Art\.?\s*\d+[ºo°]?[-A-Za-z]*\b`)
	headingRE = regexp.MustCompile(`(?m)^#{1,4}\s`)
	palavraRE = regexp.MustCompile(`[\p{L}\p{N}_]+`)
)

var arquivosNaoNormativos = map[string]bool{
	"readme.md": true, "license.md": true, "licence.md": true, "contributing.md": true,
	"changelog.md": true, "code_of_conduct.md": true, "security.md": true,
}

// SituacoesVigentes são os estados explicitamente ativos.
var SituacoesVigentes = []string{"vigente", "em vigor", "vigente parcialmente", "parcialmente vigente"}

var tabelasIndiceLeis = map[string]bool{
	"leis_docs": true, "leis_fts": true, "leis_fts_data": true, "leis_fts_idx": true,
	"leis_fts_content": true, "leis_fts_docsize": true, "leis_fts_config": true,
}

// ParseMarkdown separa o frontmatter YAML do corpo.
func ParseMarkdown(texto string) (map[string]interface{}, string) {
	m := frontRE.FindStringSubmatch(texto)
	if m == nil {
		return map[string]interface{}{}, texto
	}
	var meta map[string]interface{}
	if err := yaml.Unmarshal([]byte(m[1]), &meta); err != nil || meta == nil {
		meta = map[string]interface{}{}
	}
	return meta, texto[len(m[0]):]
}

func pick(meta map[string]interface{}, chaves ...string) string {
	for _, k := range chaves {
		if v, ok := meta[k]; ok {
			if s := strValor(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// strValor imita o str() do Python sobre valores YAML.
func strValor(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case time.Time:
		return t.Format("2006-01-02")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// metaJSON serializa o frontmatter como o Python (json.dumps, default=str).
func metaJSON(meta map[string]interface{}) string {
	conv := map[string]interface{}{}
	for k, v := range meta {
		switch t := v.(type) {
		case time.Time:
			conv[k] = t.Format("2006-01-02")
		default:
			conv[k] = v
		}
	}
	b, err := json.Marshal(conv)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Fatiar divide o corpo em trechos por artigo (ou por título), com no
// máximo maxChars caracteres cada.
func Fatiar(corpo string, maxChars int) []string {
	partes := fatiarPor(corpo, artigoRE)
	if len(partes) <= 1 {
		partes = fatiarPor(corpo, headingRE)
	}
	var saida []string
	for _, p := range partes {
		p = strings.TrimSpace(p)
		r := []rune(p)
		for len(r) > maxChars {
			corte := ultimoDuploNL(r, maxChars)
			if corte <= 0 {
				corte = maxChars
			}
			saida = append(saida, strings.TrimSpace(string(r[:corte])))
			r = []rune(strings.TrimSpace(string(r[corte:])))
		}
		if len(r) > 0 {
			saida = append(saida, string(r))
		}
	}
	return saida
}

// fatiarPor divide o texto nos inícios de cada ocorrência do padrão
// (o padrão é zero-width no Python — lookahead — então o corte é no
// início da linha; aqui usamos o início de cada match).
func fatiarPor(corpo string, re *regexp.Regexp) []string {
	locs := re.FindAllStringIndex(corpo, -1)
	if len(locs) == 0 {
		if strings.TrimSpace(corpo) == "" {
			return nil
		}
		return []string{corpo}
	}
	var partes []string
	prev := 0
	for _, loc := range locs {
		partes = append(partes, corpo[prev:loc[0]])
		prev = loc[0]
	}
	partes = append(partes, corpo[prev:])
	var out []string
	for _, p := range partes {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// ultimoDuploNL devolve o índice do último "\n\n" contido nos primeiros
// limit runes (-1 se não houver).
func ultimoDuploNL(r []rune, limit int) int {
	if limit > len(r) {
		limit = len(r)
	}
	for i := limit - 2; i >= 0; i-- {
		if r[i] == '\n' && r[i+1] == '\n' {
			return i
		}
	}
	return -1
}

// Normalizar tira acentos, minúsculas e colapsa espaços (NFKD).
func Normalizar(valor string) string {
	descomposto := norm.NFKD.String(valor)
	var b strings.Builder
	for _, r := range descomposto {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(strings.ToLower(b.String())), " ")
}

// SituacaoEhVigente só aceita estados explicitamente ativos.
func SituacaoEhVigente(situacao string) bool {
	n := Normalizar(situacao)
	for _, s := range SituacoesVigentes {
		if n == s {
			return true
		}
	}
	return false
}

func mesmoArquivo(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil && ra == rb {
		return true
	}
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ia, ib)
	}
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return aa == bb
}

// ValidarBancosSeparados recusa LEIS_DB_PATH igual ao DB_PATH.
func ValidarBancosSeparados(leisDBPath, mutableDBPath string) error {
	if mutableDBPath != "" && mesmoArquivo(leisDBPath, mutableDBPath) {
		return errors.New("LEIS_DB_PATH e DB_PATH não podem apontar para o mesmo arquivo.")
	}
	return nil
}

func dsnReadOnly(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := &url.URL{Scheme: "file", Path: abs}
	return u.String() + "?mode=ro"
}

func dsnReadWrite(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := &url.URL{Scheme: "file", Path: abs}
	return u.String()
}

// AbrirMutavel abre (criando se preciso) o banco mutável legado, com as
// tabelas de leis vazias por compatibilidade — igual ao conectar() Python.
func AbrirMutavel(dbPath string) (*sql.DB, error) {
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dsnReadWrite(dbPath))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS leis_docs(
		path TEXT PRIMARY KEY, titulo TEXT, urn TEXT, situacao TEXT, ementa TEXT, meta TEXT);
	CREATE VIRTUAL TABLE IF NOT EXISTS leis_fts USING fts5(
		texto, path UNINDEXED, titulo UNINDEXED, urn UNINDEXED, situacao UNINDEXED,
		tokenize='unicode61 remove_diacritics 2');`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func abrirSomenteLeitura(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsnReadOnly(dbPath))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func conectarLeitura(dbPath string) (*sql.DB, error) {
	if st, err := os.Stat(dbPath); err != nil || st.IsDir() {
		return nil, indiceErr("Índice de leis ausente.")
	}
	db, err := abrirSomenteLeitura(dbPath)
	if err != nil {
		return nil, indiceErr("Índice de leis indisponível.")
	}
	return db, nil
}

// IndiceDisponivel diz se há tabela FTS com ao menos um trecho pesquisável.
func IndiceDisponivel(dbPath string) bool {
	db, err := conectarLeitura(dbPath)
	if err != nil {
		return false
	}
	defer db.Close()
	var um int
	err = db.QueryRow("SELECT 1 FROM leis_fts LIMIT 1").Scan(&um)
	return err == nil
}

func validarDestinoIndice(leisDBPath string) error {
	st, err := os.Stat(leisDBPath)
	if err != nil {
		return nil // não existe: destino livre
	}
	if st.IsDir() {
		return errors.New("Destino do índice de leis não é um arquivo SQLite.")
	}
	db, err := abrirSomenteLeitura(leisDBPath)
	if err != nil {
		return errors.New("Destino do índice existente não é um SQLite legível; ingestão recusada.")
	}
	defer db.Close()
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return errors.New("Destino do índice existente não é um SQLite legível; ingestão recusada.")
	}
	defer rows.Close()
	var estranhas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if !tabelasIndiceLeis[name] {
			estranhas = append(estranhas, name)
		}
	}
	if len(estranhas) > 0 {
		sort.Strings(estranhas)
		return fmt.Errorf("Destino do índice contém tabelas fora do esquema de leis (%s); ingestão recusada.", strings.Join(estranhas, ", "))
	}
	return nil
}

func criarSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE leis_docs(
		path TEXT PRIMARY KEY, titulo TEXT, urn TEXT, situacao TEXT, ementa TEXT, meta TEXT);
	CREATE VIRTUAL TABLE leis_fts USING fts5(
		texto, path UNINDEXED, titulo UNINDEXED, urn UNINDEXED, situacao UNINDEXED,
		tokenize='unicode61 remove_diacritics 2');`)
	return err
}

func arquivoNormativo(nome string, meta map[string]interface{}, corpo string) bool {
	if arquivosNaoNormativos[strings.ToLower(nome)] {
		return false
	}
	if pick(meta, "titulo", "título", "title", "nome") == "" && pick(meta, "urn", "urn_lex", "lex") == "" {
		return false
	}
	return strings.TrimSpace(corpo) != "" && len(Fatiar(corpo, 2500)) > 0
}

type documento struct {
	rel   string
	meta  map[string]interface{}
	corpo string
}

func carregarAcervo(leisDir string) ([]documento, error) {
	st, err := os.Stat(leisDir)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("Diretório do acervo não existe ou não é diretório: %s", leisDir)
	}
	var arquivos []string
	err = filepath.WalkDir(leisDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			arquivos = append(arquivos, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(arquivos)
	var docs []documento
	for _, arquivo := range arquivos {
		bruto, err := os.ReadFile(arquivo)
		if err != nil {
			return nil, fmt.Errorf("Não foi possível ler %s: %w", arquivo, err)
		}
		texto := strings.ToValidUTF8(string(bruto), "�")
		meta, corpo := ParseMarkdown(texto)
		rel, _ := filepath.Rel(leisDir, arquivo)
		if arquivoNormativo(filepath.Base(arquivo), meta, corpo) {
			docs = append(docs, documento{rel: rel, meta: meta, corpo: corpo})
		}
	}
	if len(docs) == 0 {
		return nil, errors.New("Acervo sem documentos normativos Markdown com metadados legais; índice anterior preservado.")
	}
	return docs, nil
}

// Ingerir constrói um novo índice de leis e o publica por troca atômica.
// O banco mutável nunca é aberto. Em erro, o índice anterior permanece intacto.
func Ingerir(leisDir, leisDBPath, mutableDBPath string) (int, error) {
	if err := ValidarBancosSeparados(leisDBPath, mutableDBPath); err != nil {
		return 0, err
	}
	if err := validarDestinoIndice(leisDBPath); err != nil {
		return 0, err
	}
	documentos, err := carregarAcervo(leisDir)
	if err != nil {
		return 0, err
	}
	if dir := filepath.Dir(leisDBPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(leisDBPath), "."+filepath.Base(leisDBPath)+".*.tmp")
	if err != nil {
		return 0, err
	}
	tmp := tmpFile.Name()
	tmpFile.Close()
	n := 0
	err = func() error {
		db, err := sql.Open("sqlite", dsnReadWrite(tmp))
		if err != nil {
			return err
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		if err := criarSchema(db); err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, doc := range documentos {
			titulo := pick(doc.meta, "titulo", "título", "title", "nome")
			if titulo == "" {
				titulo = strings.TrimSuffix(filepath.Base(doc.rel), filepath.Ext(doc.rel))
			}
			urn := pick(doc.meta, "urn", "urn_lex", "lex")
			situacao := pick(doc.meta, "situacao", "situação", "status")
			ementa := pick(doc.meta, "ementa", "descricao", "description")
			if _, err := tx.Exec("INSERT INTO leis_docs VALUES(?,?,?,?,?,?)",
				doc.rel, titulo, urn, situacao, ementa, metaJSON(doc.meta)); err != nil {
				return err
			}
			for _, fatia := range Fatiar(doc.corpo, 2500) {
				if _, err := tx.Exec("INSERT INTO leis_fts(texto,path,titulo,urn,situacao) VALUES(?,?,?,?,?)",
					fatia, doc.rel, titulo, urn, situacao); err != nil {
					return err
				}
				n++
			}
		}
		if n == 0 {
			return errors.New("Acervo não produziu trechos pesquisáveis.")
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		var docs int
		if err := db.QueryRow("SELECT count(*) FROM leis_docs").Scan(&docs); err != nil {
			return err
		}
		if docs != len(documentos) {
			return errors.New("Índice temporário incompleto.")
		}
		return nil
	}()
	if err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, leisDBPath); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	slog.Info("ingestao_concluida", "fatias", n, "documentos", len(documentos), "dir", leisDir)
	return n, nil
}

// Trecho é um trecho de lei retornado pela busca. O score é interno
// (ordenação) e não vai para o JSON — o front não o exibe.
type Trecho struct {
	Texto       string  `json:"texto"`
	Path        string  `json:"path"`
	Titulo      string  `json:"titulo"`
	Urn         string  `json:"urn"`
	Situacao    string  `json:"situacao"`
	Fonte       *string `json:"fonte"`
	VersaoTexto *string `json:"versao_texto"`
	BaixadoEm   *string `json:"baixado_em"`
	Ementa      string  `json:"ementa"`
	score       float64
}

// Score devolve o BM25 do trecho (interno; só vai para os tool results
// do Claude, como o t.__dict__ do Python — nunca para /api/leis).
func (t Trecho) Score() float64 { return t.score }

// metadadosDeRetorno extrai fonte, versão do texto e data de obtenção do
// JSON de metadados do documento (nil quando ausente ou inválido).
func metadadosDeRetorno(metaJSONStr string) (*string, *string, *string) {
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(metaJSONStr), &meta); err != nil || meta == nil {
		return nil, nil, nil
	}
	strPtr := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	return strPtr(pick(meta, "fonte")),
		strPtr(pick(meta, "versao_texto")),
		strPtr(pick(meta, "baixado_em"))
}

func ftsQuery(q string) string {
	var termos []string
	for _, t := range palavraRE.FindAllString(q, -1) {
		if len([]rune(t)) > 2 {
			termos = append(termos, `"`+t+`"`)
		}
	}
	return strings.Join(termos, " OR ")
}

func termosConsulta(consulta string) []string {
	var out []string
	for _, t := range palavraRE.FindAllString(Normalizar(consulta), -1) {
		if len([]rune(t)) > 2 {
			out = append(out, t)
		}
	}
	return out
}

func casaNomeDaLei(consulta, titulo, ementa string) bool {
	alvo := Normalizar(titulo + " " + ementa)
	for _, t := range termosConsulta(consulta) {
		if strings.Contains(alvo, t) {
			return true
		}
	}
	return false
}

// relevancia ordena por cobertura da consulta: nome da lei e frase exata
// pesam mais que o bm25, que só desempata (menor = mais relevante).
func relevancia(consulta, titulo, ementa, texto string, bm25Score float64) [7]float64 {
	termos := termosConsulta(consulta)
	conta := func(alvo string) int {
		n := 0
		for _, t := range termos {
			if strings.Contains(alvo, t) {
				n++
			}
		}
		return n
	}
	tituloN := Normalizar(titulo)
	ementaN := Normalizar(ementa)
	textoN := Normalizar(texto)
	frase := ""
	if len(termos) > 1 {
		frase = Normalizar(consulta)
	}
	tem := func(f, alvo string) float64 {
		if f != "" && strings.Contains(alvo, f) {
			return 1
		}
		return 0
	}
	return [7]float64{
		-float64(conta(tituloN)),
		-float64(conta(ementaN)),
		-tem(frase, tituloN),
		-tem(frase, ementaN),
		-tem(frase, textoN),
		-float64(conta(textoN)),
		bm25Score,
	}
}

func menorRelevancia(a, b [7]float64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Buscar consulta o índice. limite deve estar entre 1 e 20.
func Buscar(dbPath, consulta string, limite int, apenasVigentes bool) ([]Trecho, error) {
	if limite < 1 || limite > 20 {
		return nil, errors.New("limite deve estar entre 1 e 20")
	}
	fq := ftsQuery(consulta)
	if fq == "" {
		return []Trecho{}, nil
	}
	db, err := conectarLeitura(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	sqlQ := `SELECT f.texto, f.path, f.titulo, f.urn, f.situacao, bm25(leis_fts) AS s, d.meta, d.ementa
		FROM leis_fts AS f
		LEFT JOIN leis_docs AS d ON d.path = f.path
		WHERE leis_fts MATCH ?`
	var args []interface{}
	args = append(args, fq)
	if apenasVigentes {
		marcadores := append([]string{}, SituacoesVigentes...)
		sort.Strings(marcadores)
		placeholders := make([]string, len(marcadores))
		for i, m := range marcadores {
			placeholders[i] = "?"
			args = append(args, m)
		}
		sqlQ += " AND trim(lower(f.situacao)) IN (" + strings.Join(placeholders, ",") + ")"
	}
	fetch := limite * 5
	if fetch > 100 {
		fetch = 100
	}
	sqlQ += " ORDER BY s LIMIT ?"
	args = append(args, fetch)

	rows, err := db.Query(sqlQ, args...)
	if err != nil {
		return nil, indiceErr("Falha ao consultar o índice de leis.")
	}
	var trechos []Trecho
	for rows.Next() {
		var t Trecho
		var meta sql.NullString
		var ementa sql.NullString
		if err := rows.Scan(&t.Texto, &t.Path, &t.Titulo, &t.Urn, &t.Situacao, &t.score, &meta, &ementa); err != nil {
			rows.Close()
			return nil, indiceErr("Falha ao consultar o índice de leis.")
		}
		t.Fonte, t.VersaoTexto, t.BaixadoEm = metadadosDeRetorno(meta.String)
		t.Ementa = ementa.String
		trechos = append(trechos, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, indiceErr("Falha ao consultar o índice de leis.")
	}

	// Busca por nome da lei: um documento cujo título/ementa casa com a
	// consulta entra mesmo que nenhum trecho dele tenha vindo no FTS.
	func() {
		vistos := map[string]bool{}
		for _, t := range trechos {
			vistos[t.Path] = true
		}
		drows, err := db.Query("SELECT path, titulo, urn, situacao, ementa, meta FROM leis_docs")
		if err != nil {
			return
		}
		type doc struct {
			path, titulo, urn, situacao, ementa, meta string
		}
		var docs []doc
		for drows.Next() {
			var d doc
			var ementa, meta sql.NullString
			if err := drows.Scan(&d.path, &d.titulo, &d.urn, &d.situacao, &ementa, &meta); err != nil {
				drows.Close()
				return
			}
			d.ementa, d.meta = ementa.String, meta.String
			docs = append(docs, d)
		}
		// Fecha antes de consultar os trechos: o pool tem 1 conexão.
		drows.Close()
		for _, d := range docs {
			if vistos[d.path] {
				continue
			}
			if apenasVigentes && !SituacaoEhVigente(d.situacao) {
				continue
			}
			if !casaNomeDaLei(consulta, d.titulo, d.ementa) {
				continue
			}
			var primeiro string
			err := db.QueryRow("SELECT texto FROM leis_fts WHERE path = ? ORDER BY rowid LIMIT 1", d.path).Scan(&primeiro)
			if err != nil {
				continue
			}
			fonte, versao, baixado := metadadosDeRetorno(d.meta)
			trechos = append(trechos, Trecho{
				Texto: primeiro, Path: d.path, Titulo: d.titulo, Urn: d.urn,
				Situacao: d.situacao, score: 0, Fonte: fonte, VersaoTexto: versao,
				BaixadoEm: baixado, Ementa: d.ementa,
			})
		}
	}()

	sort.SliceStable(trechos, func(i, j int) bool {
		ri := relevancia(consulta, trechos[i].Titulo, trechos[i].Ementa, trechos[i].Texto, trechos[i].score)
		rj := relevancia(consulta, trechos[j].Titulo, trechos[j].Ementa, trechos[j].Texto, trechos[j].score)
		return menorRelevancia(ri, rj)
	})
	if len(trechos) > limite {
		trechos = trechos[:limite]
	}
	if trechos == nil {
		trechos = []Trecho{}
	}
	return trechos, nil
}

// CruzarProcesso cruza classe/assuntos do DataJud com legislação vigente.
func CruzarProcesso(dbPath, classe string, assuntos []string, limite int) ([]Trecho, error) {
	consulta := strings.Join(append([]string{classe}, assuntos...), " ")
	return Buscar(dbPath, consulta, limite, true)
}

// HashArquivo devolve o SHA-256 de um arquivo (usado pelo indexador).
func HashArquivo(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}
