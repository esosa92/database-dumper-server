package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	RoleEditor   = "editor"
	RoleOperator = "operator"
)

type User struct {
	ID        int64
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
}

type ServerAccess struct {
	ServerID string
	Role     string
}

func createUserSchema() error {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			username      TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin      INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS user_servers (
			user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
			role      TEXT NOT NULL,
			PRIMARY KEY (user_id, server_id)
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT PRIMARY KEY,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TEXT NOT NULL
		);
	`)
	return err
}

func ValidRole(role string) bool {
	return role == RoleEditor || role == RoleOperator
}

func hashPassword(password string) (string, error) {
	if len(password) < 4 {
		return "", errors.New("password must be at least 4 characters")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

func CreateUser(username, password string, isAdmin bool) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, errors.New("username is required")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC()
	res, err := DB.Exec(`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		username, hash, isAdmin, now.Format(time.RFC3339))
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Username: username, IsAdmin: isAdmin, CreatedAt: now}, nil
}

func scanUser(row scanner) (User, error) {
	var u User
	var created string
	if err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &created); err != nil {
		return u, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

const userColumns = "id, username, is_admin, created_at"

func GetUserByID(id int64) (User, error) {
	return scanUser(DB.QueryRow("SELECT "+userColumns+" FROM users WHERE id = ?", id))
}

func GetUserByUsername(username string) (User, error) {
	return scanUser(DB.QueryRow("SELECT "+userColumns+" FROM users WHERE username = ?", username))
}

func ListUsers() ([]User, error) {
	rows, err := DB.Query("SELECT " + userColumns + " FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func CountUsers() (int, error) {
	var n int
	err := DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

func UpdateUser(id int64, username string, isAdmin bool) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username is required")
	}
	_, err := DB.Exec("UPDATE users SET username = ?, is_admin = ? WHERE id = ?", username, isAdmin, id)
	return err
}

func SetPassword(id int64, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	res, err := DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", hash, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func DeleteUser(id int64) error {
	_, err := DB.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func Authenticate(username, password string) (User, error) {
	var u User
	var hash, created string
	err := DB.QueryRow("SELECT id, username, is_admin, created_at, password_hash FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.IsAdmin, &created, &hash)
	if err != nil {
		return u, errors.New("invalid username or password")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return u, errors.New("invalid username or password")
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

func GetUserServers(userID int64) ([]ServerAccess, error) {
	rows, err := DB.Query("SELECT server_id, role FROM user_servers WHERE user_id = ? ORDER BY server_id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServerAccess
	for rows.Next() {
		var a ServerAccess
		if err := rows.Scan(&a.ServerID, &a.Role); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func SetUserServers(userID int64, access []ServerAccess) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM user_servers WHERE user_id = ?", userID); err != nil {
		return err
	}
	for _, a := range access {
		if !ValidRole(a.Role) {
			return fmt.Errorf("invalid role %q for server %s", a.Role, a.ServerID)
		}
		if _, err := tx.Exec("INSERT INTO user_servers (user_id, server_id, role) VALUES (?, ?, ?)", userID, a.ServerID, a.Role); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func GrantServer(userID int64, serverID, role string) error {
	_, err := DB.Exec("INSERT OR REPLACE INTO user_servers (user_id, server_id, role) VALUES (?, ?, ?)", userID, serverID, role)
	return err
}

func ServerRole(userID int64, serverID string) (string, error) {
	var role string
	err := DB.QueryRow("SELECT role FROM user_servers WHERE user_id = ? AND server_id = ?", userID, serverID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

const sessionTTL = 30 * 24 * time.Hour

func CreateSession(userID int64) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	expires := time.Now().Add(sessionTTL).UTC().Format(time.RFC3339)
	if _, err := DB.Exec("INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)", token, userID, expires); err != nil {
		return "", err
	}
	return token, nil
}

func GetSessionUser(token string) (User, error) {
	var userID int64
	var expires string
	err := DB.QueryRow("SELECT user_id, expires_at FROM sessions WHERE token = ?", token).Scan(&userID, &expires)
	if err != nil {
		return User{}, err
	}
	if t, err := time.Parse(time.RFC3339, expires); err != nil || t.Before(time.Now()) {
		DeleteSession(token)
		return User{}, sql.ErrNoRows
	}
	return GetUserByID(userID)
}

func DeleteSession(token string) error {
	_, err := DB.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

func DeleteUserSessions(userID int64) error {
	_, err := DB.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}
