// Package cotas implementa cotas e créditos do bot: franquia grátis
// diária + créditos pré-pagos (unidade: tokens do LLM). Chaves são
// guardadas só como hash SHA-256. Também faz o limite de taxa por IP
// (janela deslizante em memória, 1 processo).
package cotas

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
)

var (
	mu      sync.Mutex
	janelas = map[string][]time.Time{}
)

// Limiter é o limite de taxa por IP (janela deslizante em memória, 1
// processo). Um por servidor; o estado não é global para os testes
// poderem criar servidores independentes.
type Limiter struct {
	mu      sync.Mutex
	janelas map[string][]time.Time
}

// NewLimiter cria um limitador vazio.
func NewLimiter() *Limiter { return &Limiter{janelas: map[string][]time.Time{}} }

// Allow implementa a janela deslizante por IP. True = permitido.
func (l *Limiter) Allow(ip string, maximo int, janela time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	agora := time.Now()
	q := l.janelas[ip]
	var mantidos []time.Time
	for _, t := range q {
		if agora.Sub(t) <= janela {
			mantidos = append(mantidos, t)
		}
	}
	if len(mantidos) >= maximo {
		l.janelas[ip] = mantidos
		return false
	}
	l.janelas[ip] = append(mantidos, agora)
	return true
}

// LimiteTaxa é o atalho do limitador global (compatibilidade).
func LimiteTaxa(ip string, maximo int, janela time.Duration) bool {
	mu.Lock()
	defer mu.Unlock()
	agora := time.Now()
	q := janelas[ip]
	var mantidos []time.Time
	for _, t := range q {
		if agora.Sub(t) <= janela {
			mantidos = append(mantidos, t)
		}
	}
	if len(mantidos) >= maximo {
		janelas[ip] = mantidos
		return false
	}
	janelas[ip] = append(mantidos, agora)
	return true
}

// LimparJanelas esvazia o estado do limitador global (usado nos testes).
func LimparJanelas() {
	mu.Lock()
	defer mu.Unlock()
	janelas = map[string][]time.Time{}
}

func hash(chave string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(chave)))
}

func abrir(dbPath string) (*sql.DB, error) {
	db, err := leis.AbrirMutavel(dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS contas(
		h TEXT PRIMARY KEY, creditos INTEGER DEFAULT 0,
		gratis_usado INTEGER DEFAULT 0, dia TEXT, rotulo TEXT)`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// CriarChave emite uma chave "oj_..." e a guarda só como hash.
func CriarChave(dbPath, rotulo string, creditos int) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	chave := "oj_" + base64.RawURLEncoding.EncodeToString(raw)
	db, err := abrir(dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	hoje := time.Now().Format("2006-01-02")
	_, err = db.Exec("INSERT INTO contas(h,creditos,dia,rotulo) VALUES(?,?,?,?)",
		hash(chave), creditos, hoje, rotulo)
	if err != nil {
		return "", err
	}
	return chave, nil
}

// AdicionarCreditos soma créditos a uma chave. Nil se a chave não existe.
func AdicionarCreditos(dbPath, chave string, tokens int) (*int, error) {
	db, err := abrir(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	res, err := db.Exec("UPDATE contas SET creditos=creditos+? WHERE h=?", tokens, hash(chave))
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, nil
	}
	var novo int
	if err := db.QueryRow("SELECT creditos FROM contas WHERE h=?", hash(chave)).Scan(&novo); err != nil {
		return nil, err
	}
	return &novo, nil
}

// Saldo devolve créditos e franquia restante do dia. Nil = chave inválida.
// Reseta a franquia grátis a cada dia.
type Saldo struct {
	Creditos       int `json:"creditos"`
	GratisRestante int `json:"gratis_restante"`
}

func SaldoConta(dbPath, chave string, franquia int) (*Saldo, error) {
	db, err := abrir(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var creditos, gratisUsado int
	var dia string
	err = db.QueryRow("SELECT creditos,gratis_usado,dia FROM contas WHERE h=?", hash(chave)).
		Scan(&creditos, &gratisUsado, &dia)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hoje := time.Now().Format("2006-01-02")
	usado := gratisUsado
	if dia != hoje {
		usado = 0
		if _, err := db.Exec("UPDATE contas SET gratis_usado=0, dia=? WHERE h=?", hoje, hash(chave)); err != nil {
			return nil, err
		}
	}
	restante := franquia - usado
	if restante < 0 {
		restante = 0
	}
	return &Saldo{Creditos: creditos, GratisRestante: restante}, nil
}

// Debitar consome primeiro a franquia grátis do dia; o excedente sai dos
// créditos (mínimo 0).
func Debitar(dbPath, chave string, tokens, franquia int) error {
	s, err := SaldoConta(dbPath, chave, franquia)
	if err != nil || s == nil {
		return err
	}
	doGratis := tokens
	if doGratis > s.GratisRestante {
		doGratis = s.GratisRestante
	}
	doPago := tokens - doGratis
	db, err := abrir(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("UPDATE contas SET gratis_usado=gratis_usado+?, creditos=MAX(0,creditos-?) WHERE h=?",
		doGratis, doPago, hash(chave))
	return err
}
