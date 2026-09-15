package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init(path string) error {
	var err error
	DB, err = sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	if _, err := DB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := createSchema(); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	if err := createUserSchema(); err != nil {
		return fmt.Errorf("create user schema: %w", err)
	}

	if err := createJobSchema(); err != nil {
		return fmt.Errorf("create job schema: %w", err)
	}

	return nil
}

func createSchema() error {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS servers (
			id                         TEXT PRIMARY KEY,
			ssh_host                   TEXT NOT NULL,
			ssh_pass                   TEXT NOT NULL DEFAULT '',
			remote_env_path            TEXT NOT NULL,
			local_path                 TEXT NOT NULL DEFAULT '',
			enabled                    INTEGER NOT NULL DEFAULT 1,
			with_core_config           INTEGER NOT NULL DEFAULT 0,
			only_core_config           INTEGER NOT NULL DEFAULT 0,
			enable_set_gtid_purged_off INTEGER NOT NULL DEFAULT 0,
			ignore_tables              TEXT NOT NULL DEFAULT '[]',
			only_tables                TEXT NOT NULL DEFAULT '',
			net_buffer_length          TEXT NOT NULL DEFAULT '',
			skip_extended_insert       INTEGER NOT NULL DEFAULT 0,
			skip_add_locks             INTEGER NOT NULL DEFAULT 0,
			skip_disable_keys          INTEGER NOT NULL DEFAULT 0,
			skip_lock_tables           INTEGER NOT NULL DEFAULT 0,
			skip_add_drop_table        INTEGER NOT NULL DEFAULT 0,
			single_table_mode          INTEGER NOT NULL DEFAULT 0,
			dump_client                TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		return err
	}

	extra := map[string]string{
		"skip_disable_keys":   "INTEGER NOT NULL DEFAULT 0",
		"skip_lock_tables":    "INTEGER NOT NULL DEFAULT 0",
		"skip_add_drop_table": "INTEGER NOT NULL DEFAULT 0",
		"single_table_mode":   "INTEGER NOT NULL DEFAULT 0",
		"dump_client":         "TEXT NOT NULL DEFAULT ''",
	}
	existing, err := columnNames("servers")
	if err != nil {
		return err
	}
	for col, def := range extra {
		if existing[col] {
			continue
		}
		if _, err := DB.Exec(fmt.Sprintf("ALTER TABLE servers ADD COLUMN %s %s", col, def)); err != nil {
			return fmt.Errorf("add column %s: %w", col, err)
		}
	}
	return nil
}

func columnNames(table string) (map[string]bool, error) {
	rows, err := DB.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}
