package db

import (
	"encoding/json"
	"time"
)

type JobRecord struct {
	ID        int64
	ServerID  string
	Status    string
	StartedAt time.Time
	EndedAt   time.Time
	Error     string
	Files     []string
	Log       string
}

func createJobSchema() error {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS jobs (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id  TEXT NOT NULL,
			status     TEXT NOT NULL,
			started_at TEXT NOT NULL,
			ended_at   TEXT NOT NULL DEFAULT '',
			error      TEXT NOT NULL DEFAULT '',
			files      TEXT NOT NULL DEFAULT '[]',
			log        TEXT NOT NULL DEFAULT ''
		)
	`)
	return err
}

func InsertJob(serverID, status string, started time.Time) (int64, error) {
	res, err := DB.Exec(`INSERT INTO jobs (server_id, status, started_at) VALUES (?, ?, ?)`,
		serverID, status, started.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateJob(j JobRecord) error {
	files, err := json.Marshal(j.Files)
	if err != nil {
		return err
	}
	if j.Files == nil {
		files = []byte("[]")
	}
	ended := ""
	if !j.EndedAt.IsZero() {
		ended = j.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = DB.Exec(`UPDATE jobs SET status = ?, ended_at = ?, error = ?, files = ?, log = ? WHERE id = ?`,
		j.Status, ended, j.Error, string(files), j.Log, j.ID)
	return err
}

func MarkInterruptedJobs() (int64, error) {
	res, err := DB.Exec(`UPDATE jobs SET status = 'interrupted', error = 'server restarted while running', ended_at = ?
		WHERE status = 'running'`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const jobColumns = "id, server_id, status, started_at, ended_at, error, files, log"

func scanJob(row scanner) (JobRecord, error) {
	var j JobRecord
	var started, ended, files string
	if err := row.Scan(&j.ID, &j.ServerID, &j.Status, &started, &ended, &j.Error, &files, &j.Log); err != nil {
		return j, err
	}
	j.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	if ended != "" {
		j.EndedAt, _ = time.Parse(time.RFC3339Nano, ended)
	}
	if err := json.Unmarshal([]byte(files), &j.Files); err != nil || j.Files == nil {
		j.Files = []string{}
	}
	return j, nil
}

func GetJob(id int64) (JobRecord, error) {
	return scanJob(DB.QueryRow("SELECT "+jobColumns+" FROM jobs WHERE id = ?", id))
}

func ListJobsFor(u User, limit int) ([]JobRecord, error) {
	query := "SELECT " + jobColumns + " FROM jobs"
	var args []any
	if !u.IsAdmin {
		query += " WHERE server_id IN (SELECT server_id FROM user_servers WHERE user_id = ?)"
		args = append(args, u.ID)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []JobRecord
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func DeleteJob(id int64) error {
	_, err := DB.Exec("DELETE FROM jobs WHERE id = ?", id)
	return err
}
