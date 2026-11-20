// Package aprender implementa o aprendizado recorrente SEM LLM: o bot
// aprende respostas ensinadas por pessoas. Fluxo: ensinar -> fica
// pendente -> admin aprova -> passa a ser servido por similaridade.
// Perguntas sem resposta viram lacunas (ordenadas por frequência).
package aprender

import (
	"database/sql"
	"regexp"
	"strings"
	"unicode"

	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
	"golang.org/x/text/unicode/norm"
)

var tokenRE = regexp.MustCompile(`[a-z0-9]+`)

// Tokens extrai os termos pesquisáveis de um texto (normalizado, len > 2).
func Tokens(txt string) map[string]bool {
	descomposto := norm.NFD.String(strings.ToLower(txt))
	var b strings.Builder
	for _, r := range descomposto {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	out := map[string]bool{}
	for _, t := range tokenRE.FindAllString(b.String(), -1) {
		if len(t) > 2 {
			out[t] = true
		}
	}
	return out
}

// Similaridade é o índice de Jaccard entre os tokens de dois textos.
func Similaridade(a, b string) float64 {
	ta, tb := Tokens(a), Tokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter, uniao := 0, len(ta)
	for t := range tb {
		if ta[t] {
			inter++
		} else {
			uniao++
		}
	}
	return float64(inter) / float64(uniao)
}

func abrir(dbPath string) (*sql.DB, error) {
	db, err := leis.AbrirMutavel(dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS conhecimento(
		id INTEGER PRIMARY KEY, pergunta TEXT, resposta TEXT,
		aprovado INTEGER DEFAULT 0, usos INTEGER DEFAULT 0);
	CREATE TABLE IF NOT EXISTS lacunas(texto TEXT PRIMARY KEY, n INTEGER DEFAULT 1);`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func truncarRunas(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

// Ensinar registra uma pergunta/resposta como pendente de moderação.
func Ensinar(dbPath, pergunta, resposta string) (int64, error) {
	db, err := abrir(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	res, err := db.Exec("INSERT INTO conhecimento(pergunta,resposta) VALUES(?,?)",
		truncarRunas(pergunta, 300), truncarRunas(resposta, 1500))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Aprovar marca um ensinamento como aprovado.
func Aprovar(dbPath string, id int64) (bool, error) {
	db, err := abrir(dbPath)
	if err != nil {
		return false, err
	}
	defer db.Close()
	res, err := db.Exec("UPDATE conhecimento SET aprovado=1 WHERE id=?", id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ItemPendente é um ensinamento aguardando moderação.
type ItemPendente struct {
	ID       int64  `json:"id"`
	Pergunta string `json:"pergunta"`
	Resposta string `json:"resposta"`
}

// Pendentes lista os ensinamentos ainda não aprovados.
func Pendentes(dbPath string) ([]ItemPendente, error) {
	db, err := abrir(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT id,pergunta,resposta FROM conhecimento WHERE aprovado=0 ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ItemPendente{}
	for rows.Next() {
		var it ItemPendente
		if err := rows.Scan(&it.ID, &it.Pergunta, &it.Resposta); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Buscar devolve a resposta aprovada mais similar (Jaccard >= 0.6), ou "".
// Cada acerto incrementa o contador de usos.
func Buscar(dbPath, texto string, minimo float64) (string, error) {
	db, err := abrir(dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.Query("SELECT id,pergunta,resposta FROM conhecimento WHERE aprovado=1")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	melhor, varResp := 0.0, ""
	var rid int64
	for rows.Next() {
		var id int64
		var pergunta, resposta string
		if err := rows.Scan(&id, &pergunta, &resposta); err != nil {
			return "", err
		}
		if s := Similaridade(texto, pergunta); s > melhor {
			melhor, varResp, rid = s, resposta, id
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if varResp != "" && melhor >= minimo {
		_, _ = db.Exec("UPDATE conhecimento SET usos=usos+1 WHERE id=?", rid)
		return varResp, nil
	}
	return "", nil
}

// RegistrarLacuna anota uma pergunta sem resposta (vira pauta de ensino).
func RegistrarLacuna(dbPath, texto string) error {
	db, err := abrir(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("INSERT INTO lacunas(texto) VALUES(?) ON CONFLICT(texto) DO UPDATE SET n=n+1",
		truncarRunas(texto, 300))
	return err
}

// Lacuna é uma pergunta sem resposta, com sua frequência.
type Lacuna struct {
	Texto string `json:"texto"`
	N     int    `json:"n"`
}

// Lacunas lista as perguntas sem resposta, das mais frequentes.
func Lacunas(dbPath string, limite int) ([]Lacuna, error) {
	db, err := abrir(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT texto,n FROM lacunas ORDER BY n DESC LIMIT ?", limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lacuna{}
	for rows.Next() {
		var l Lacuna
		if err := rows.Scan(&l.Texto, &l.N); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
