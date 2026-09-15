package db

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type dumpJSONEntry struct {
	Server
	IgnoreTables json.RawMessage `json:"ignore_tables"`
	OnlyTables   string          `json:"only_tables"`
}

func MigrateFromDumpJSON(jsonPath string) error {
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM servers").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		log.Printf("db: %d servers already present, skipping migration", count)
		return nil
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", jsonPath, err)
	}

	var entries []dumpJSONEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse %s: %w", jsonPath, err)
	}

	baseDir := filepath.Dir(jsonPath)

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, e := range entries {
		s := e.Server
		if s.ID == "" {
			s.ID = fmt.Sprintf("%s_%d", strings.ReplaceAll(s.SSHHost, "-", "_"), i)
		}

		s.IgnoreTables, err = resolveIgnoreTables(e.IgnoreTables, baseDir)
		if err != nil {
			return fmt.Errorf("server %q: %w", s.ID, err)
		}

		if e.OnlyTables != "" {
			content, err := os.ReadFile(resolvePath(e.OnlyTables, baseDir))
			if err != nil {
				return fmt.Errorf("server %q: read only_tables file: %w", s.ID, err)
			}
			s.OnlyTables = strings.TrimSpace(string(content))
		}

		if err := insertServerTx(tx, s); err != nil {
			return fmt.Errorf("insert server %q: %w", s.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Printf("db: migrated %d servers from %s", len(entries), jsonPath)
	return nil
}

func resolveIgnoreTables(raw json.RawMessage, baseDir string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var file string
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("ignore_tables must be an array or a file path")
	}
	content, err := os.ReadFile(resolvePath(file, baseDir))
	if err != nil {
		return nil, fmt.Errorf("read ignore_tables file: %w", err)
	}
	var tables []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			tables = append(tables, line)
		}
	}
	return tables, nil
}

func resolvePath(p, baseDir string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return filepath.Join(baseDir, p)
}
