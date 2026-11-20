// Command index constrói o índice SQLite de leis a partir do corpus
// versionado em data/leis_iniciais + manifesto data/FONTE_LEIS.json.
// Substitui o build Python (scripts/build_leis_iniciais.sh + python -m
// openjus.leis). Não toca no banco mutável (DB_PATH).
//
// Uso:
//	openjus-index --dir data/leis_iniciais --leis-db data/leis.db \
//	  --mutable-db data/openjus.db --manifest data/FONTE_LEIS.json
//
// O destino deve ser novo: um artefato de build não sobrescreve índice
// nem banco legado por engano (apague data/leis.db para reconstruir).
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/thiagollipe-web/open_jus/api-go/internal/leis"
)

type itemManifesto struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type manifesto struct {
	Files []itemManifesto `json:"files"`
}

func envOu(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func verificarManifesto(dir, caminho string) (*manifesto, error) {
	bruto, err := os.ReadFile(caminho)
	if err != nil {
		return nil, fmt.Errorf("manifesto de fonte ausente: %s", caminho)
	}
	var m manifesto
	if err := json.Unmarshal(bruto, &m); err != nil {
		return nil, fmt.Errorf("manifesto inválido: %w", err)
	}
	if len(m.Files) == 0 {
		return nil, fmt.Errorf("manifesto sem documentos")
	}
	esperados := map[string]bool{}
	for _, item := range m.Files {
		esperados[item.Path] = true
		caminhoArq := filepath.Join(dir, item.Path)
		conteudo, err := os.ReadFile(caminhoArq)
		if err != nil {
			return nil, fmt.Errorf("documento ausente: %s", caminhoArq)
		}
		if int64(len(conteudo)) != item.Bytes {
			return nil, fmt.Errorf("tamanho inesperado: %s", caminhoArq)
		}
		if fmt.Sprintf("%x", sha256.Sum256(conteudo)) != item.SHA256 {
			return nil, fmt.Errorf("SHA-256 inesperado: %s", caminhoArq)
		}
	}
	var encontrados []string
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			rel, _ := filepath.Rel(dir, path)
			encontrados = append(encontrados, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(encontrados) != len(m.Files) {
		sort.Strings(encontrados)
		return nil, fmt.Errorf("acervo inicial deve conter exatamente os Markdown do manifesto (%d no disco, %d no manifesto)", len(encontrados), len(m.Files))
	}
	return &m, nil
}

func validarIndice(caminho string, esperado int) (int, error) {
	abs, err := filepath.Abs(caminho)
	if err != nil {
		abs = caminho
	}
	db, err := sql.Open("sqlite", "file:"+abs+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var docs, trechos, semTrecho int
	if err := db.QueryRow("SELECT count(*) FROM leis_docs").Scan(&docs); err != nil {
		return 0, err
	}
	if err := db.QueryRow("SELECT count(*) FROM leis_fts").Scan(&trechos); err != nil {
		return 0, err
	}
	if err := db.QueryRow(`SELECT count(*) FROM leis_docs d
		LEFT JOIN (SELECT path, count(*) n FROM leis_fts GROUP BY path) f ON f.path = d.path
		WHERE coalesce(f.n, 0) = 0`).Scan(&semTrecho); err != nil {
		return 0, err
	}
	if docs != esperado || trechos < esperado || semTrecho != 0 {
		return 0, fmt.Errorf("validação do índice falhou: docs=%d, trechos=%d, sem_trecho=%d", docs, trechos, semTrecho)
	}
	return trechos, nil
}

func main() {
	dir := flag.String("dir", envOu("LEIS_INICIAIS_DIR", "data/leis_iniciais"), "Diretório do acervo Markdown")
	leisDB := flag.String("leis-db", envOu("LEIS_DB_PATH", "data/leis.db"), "Destino do índice SQLite")
	mutavelDB := flag.String("mutable-db", envOu("DB_PATH", "data/openjus.db"), "Banco mutável (nunca é alterado)")
	manifest := flag.String("manifest", envOu("LEIS_FONTE_MANIFEST", "data/FONTE_LEIS.json"), "Manifesto de fontes (SHA-256/bytes)")
	flag.Parse()

	if st, err := os.Stat(*dir); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "Acervo inicial ausente: %s\n", *dir)
		os.Exit(1)
	}
	if _, err := os.Stat(*leisDB); err == nil {
		fmt.Fprintf(os.Stderr, "Recusa de segurança: índice de destino já existe: %s\n", *leisDB)
		os.Exit(1)
	}
	m, err := verificarManifesto(*dir, *manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	n, err := leis.Ingerir(*dir, *leisDB, *mutavelDB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	trechos, err := validarIndice(*leisDB, len(m.Files))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = n
	fmt.Printf("Índice inicial validado: docs=%d, trechos=%d.\n", len(m.Files), trechos)
}
