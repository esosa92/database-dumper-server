package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

type Server struct {
	ID                     string   `json:"id"`
	SSHHost                string   `json:"ssh_host"`
	SSHPort                int      `json:"ssh_port"`
	SSHPass                string   `json:"ssh_pass"`
	RemoteEnvPath          string   `json:"remote_env_path"`
	LocalPath              string   `json:"local_path"`
	Enabled                bool     `json:"enabled"`
	WithCoreConfig         bool     `json:"with_core_config"`
	OnlyCoreConfig         bool     `json:"only_core_config"`
	EnableSetGTIDPurgedOff bool     `json:"enable_set_gtid_purged_off"`
	IgnoreTables           []string `json:"-"`
	OnlyTables             string   `json:"-"`
	NetBufferLength        string   `json:"net_buffer_length"`
	SkipExtendedInsert     bool     `json:"skip_extended_insert"`
	SkipAddLocks           bool     `json:"skip_add_locks"`
	SkipDisableKeys        bool     `json:"skip_disable_keys"`
	SkipLockTables         bool     `json:"skip_lock_tables"`
	SkipAddDropTable       bool     `json:"skip_add_drop_table"`
	SingleTableMode        bool     `json:"single_table_mode"`
	DumpClient             string   `json:"dump_client"`
}

const serverColumns = `id, ssh_host, ssh_pass, remote_env_path, local_path,
	enabled, with_core_config, only_core_config, enable_set_gtid_purged_off,
	ignore_tables, only_tables, net_buffer_length, skip_extended_insert, skip_add_locks,
	skip_disable_keys, skip_lock_tables, skip_add_drop_table, single_table_mode, dump_client, ssh_port`

type scanner interface {
	Scan(dest ...any) error
}

func scanServer(row scanner) (Server, error) {
	var s Server
	var ignoreTables string
	err := row.Scan(&s.ID, &s.SSHHost, &s.SSHPass, &s.RemoteEnvPath, &s.LocalPath,
		&s.Enabled, &s.WithCoreConfig, &s.OnlyCoreConfig, &s.EnableSetGTIDPurgedOff,
		&ignoreTables, &s.OnlyTables, &s.NetBufferLength, &s.SkipExtendedInsert, &s.SkipAddLocks,
		&s.SkipDisableKeys, &s.SkipLockTables, &s.SkipAddDropTable, &s.SingleTableMode, &s.DumpClient, &s.SSHPort)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal([]byte(ignoreTables), &s.IgnoreTables); err != nil || s.IgnoreTables == nil {
		s.IgnoreTables = []string{}
	}
	return s, nil
}

func serverArgs(s Server) ([]any, error) {
	ignoreTables, err := json.Marshal(s.IgnoreTables)
	if err != nil {
		return nil, fmt.Errorf("marshal ignore_tables: %w", err)
	}
	port := s.SSHPort
	if port <= 0 {
		port = 22
	}
	return []any{
		s.SSHHost, s.SSHPass, s.RemoteEnvPath, s.LocalPath,
		s.Enabled, s.WithCoreConfig, s.OnlyCoreConfig, s.EnableSetGTIDPurgedOff,
		string(ignoreTables), s.OnlyTables, s.NetBufferLength, s.SkipExtendedInsert, s.SkipAddLocks,
		s.SkipDisableKeys, s.SkipLockTables, s.SkipAddDropTable, s.SingleTableMode, s.DumpClient, port,
	}, nil
}

func GetAllServers() ([]Server, error) {
	return SearchServers("")
}

func SearchServers(q string) ([]Server, error) {
	return SearchServersFor(q, User{IsAdmin: true})
}

func SearchServersFor(q string, u User) ([]Server, error) {
	query := "SELECT " + serverColumns + " FROM servers WHERE 1=1"
	var args []any
	if !u.IsAdmin {
		query += " AND id IN (SELECT server_id FROM user_servers WHERE user_id = ?)"
		args = append(args, u.ID)
	}
	if q != "" {
		like := "%" + q + "%"
		query += " AND (id LIKE ? OR ssh_host LIKE ?)"
		args = append(args, like, like)
	}
	rows, err := DB.Query(query+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}
	return servers, rows.Err()
}

func GetServerByID(id string) (Server, error) {
	return scanServer(DB.QueryRow("SELECT "+serverColumns+" FROM servers WHERE id = ?", id))
}

type dbExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func InsertServer(s Server) error {
	return insertServerTx(DB, s)
}

func insertServerTx(tx dbExecer, s Server) error {
	args, err := serverArgs(s)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO servers (ssh_host, ssh_pass, remote_env_path, local_path,
			enabled, with_core_config, only_core_config, enable_set_gtid_purged_off,
			ignore_tables, only_tables, net_buffer_length, skip_extended_insert, skip_add_locks,
			skip_disable_keys, skip_lock_tables, skip_add_drop_table, single_table_mode, dump_client, ssh_port, id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		append(args, s.ID)...)
	return err
}

func UpdateServer(s Server) error {
	args, err := serverArgs(s)
	if err != nil {
		return err
	}
	_, err = DB.Exec(`
		UPDATE servers SET
			ssh_host = ?, ssh_pass = ?, remote_env_path = ?, local_path = ?,
			enabled = ?, with_core_config = ?, only_core_config = ?, enable_set_gtid_purged_off = ?,
			ignore_tables = ?, only_tables = ?, net_buffer_length = ?, skip_extended_insert = ?, skip_add_locks = ?,
			skip_disable_keys = ?, skip_lock_tables = ?, skip_add_drop_table = ?, single_table_mode = ?, dump_client = ?, ssh_port = ?
		WHERE id = ?`,
		append(args, s.ID)...)
	return err
}

func DeleteServer(id string) error {
	_, err := DB.Exec("DELETE FROM servers WHERE id = ?", id)
	return err
}
