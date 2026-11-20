// Package memoria é a camada 2 (Memória): snapshots de andamentos e
// sessões, com tratamento LGPD. Processos sigilosos (nivelSigilo > 0) NÃO
// têm movimentos/partes persistidos: guarda-se apenas o hash do número e
// o fato da consulta (Res. CNJ 331/2020 e LGPD art. 6º, III).
package memoria

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/thiagollipe-web/open_jus/api-go/internal/datajud"
	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
)

var cpfRE = regexp.MustCompile(`\b\d{3}\.?\d{3}\.?\d{3}-?\d{2}\b`)

// MascararPII oculta CPFs em texto livre.
func MascararPII(texto string) string {
	return cpfRE.ReplaceAllString(texto, "[CPF OCULTO]")
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS snapshots(
		id INTEGER PRIMARY KEY, numero_hash TEXT, numero TEXT, sigiloso INTEGER,
		capturado_em TEXT, payload TEXT);
	CREATE TABLE IF NOT EXISTS sessoes(
		id INTEGER PRIMARY KEY, sessao TEXT, evento TEXT, criado_em TEXT);`)
	return err
}

// movimentoJSON é a forma persistida de um movimento no snapshot.
type movimentoJSON struct {
	Codigo *int   `json:"codigo,omitempty"`
	Nome   string `json:"nome"`
	Data   string `json:"data,omitempty"`
}

type payloadJSON struct {
	Classe        string         `json:"classe,omitempty"`
	Assuntos      []string       `json:"assuntos,omitempty"`
	OrgaoJulgador string         `json:"orgao_julgador,omitempty"`
	Movimentos    []movimentoJSON `json:"movimentos,omitempty"`
	Sigiloso      bool           `json:"sigiloso,omitempty"`
}

// SalvarSnapshot persiste o resumo do processo (ou só o fato da consulta,
// se sigiloso) e devolve o payload gravado.
func SalvarSnapshot(dbPath string, p *datajud.ProcessoResumo) (map[string]interface{}, error) {
	db, err := leis.AbrirMutavel(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := initSchema(db); err != nil {
		return nil, err
	}
	sig := p.NivelSigilo > 0
	h := fmt.Sprintf("%x", sha256.Sum256([]byte(p.Numero)))
	var payload map[string]interface{}
	var numero *string
	if sig {
		payload = map[string]interface{}{"sigiloso": true}
	} else {
		movs := make([]movimentoJSON, 0, len(p.Movimentos))
		for _, m := range p.Movimentos {
			movs = append(movs, movimentoJSON{Codigo: m.Codigo, Nome: MascararPII(m.Nome), Data: m.Data})
		}
		pj := payloadJSON{Classe: p.Classe, Assuntos: p.Assuntos, OrgaoJulgador: p.OrgaoJulgador, Movimentos: movs}
		raw, _ := json.Marshal(pj)
		_ = json.Unmarshal(raw, &payload)
		numero = &p.Numero
	}
	raw, _ := json.Marshal(payload)
	_, err = db.Exec(`INSERT INTO snapshots(numero_hash,numero,sigiloso,capturado_em,payload)
		VALUES(?,?,?,?,?)`, h, numero, boolToInt(sig), time.Now().UTC().Format(time.RFC3339Nano), string(raw))
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// NovosMovimentos compara com o último snapshot e devolve os nomes dos
// movimentos inéditos desde a última consulta.
func NovosMovimentos(dbPath string, p *datajud.ProcessoResumo) ([]string, error) {
	if p.NivelSigilo > 0 {
		return []string{}, nil
	}
	db, err := leis.AbrirMutavel(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := initSchema(db); err != nil {
		return nil, err
	}
	var raw string
	err = db.QueryRow("SELECT payload FROM snapshots WHERE numero=? ORDER BY id DESC LIMIT 1", p.Numero).Scan(&raw)
	if err == sql.ErrNoRows {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var antigo struct {
		Movimentos []movimentoJSON `json:"movimentos"`
	}
	if err := json.Unmarshal([]byte(raw), &antigo); err != nil {
		return nil, err
	}
	vistos := map[string]bool{}
	for _, m := range antigo.Movimentos {
		vistos[m.Nome+"\x00"+m.Data] = true
	}
	var novos []string
	for _, m := range p.Movimentos {
		if !vistos[MascararPII(m.Nome)+"\x00"+m.Data] {
			novos = append(novos, m.Nome)
		}
	}
	if novos == nil {
		novos = []string{}
	}
	return novos, nil
}

// RegistrarEvento grava um evento de sessão (usado pelos testes e pelo MCP).
func RegistrarEvento(dbPath, sessao, evento string) error {
	db, err := leis.AbrirMutavel(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := initSchema(db); err != nil {
		return err
	}
	_, err = db.Exec("INSERT INTO sessoes(sessao,evento,criado_em) VALUES(?,?,?)",
		sessao, evento, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
