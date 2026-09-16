package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"database-dumper-server/db"
	"database-dumper-server/dumper"
	"database-dumper-server/handlers"
	"database-dumper-server/sshkey"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	dataDir := envOr("DUMPER_DATA_DIR", ".")
	listen := envOr("DUMPER_LISTEN", ":8080")

	if err := db.Init(filepath.Join(dataDir, "dumper.db")); err != nil {
		log.Fatalf("db init: %v", err)
	}

	if len(os.Args) > 1 {
		if err := runCLI(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	tmpl, err := initTemplates()
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	if err := db.MigrateFromDumpJSON(filepath.Join(dataDir, "dump.json")); err != nil {
		log.Printf("migration skipped: %v", err)
	}

	if err := bootstrapAdmin(); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}

	if n, err := db.MarkInterruptedJobs(); err != nil {
		log.Printf("mark interrupted jobs: %v", err)
	} else if n > 0 {
		log.Printf("marked %d running jobs as interrupted", n)
	}

	keys := sshkey.New(dataDir)
	if created, err := keys.EnsureExists("database-dumper"); err != nil {
		log.Fatalf("ssh key: %v", err)
	} else if created {
		log.Printf("generated ssh key at %s", keys.PrivateKeyPath())
	}

	h := handlers.New(tmpl, dumper.NewManager(keys), keys)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /login", h.LoginForm)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("POST /logout", h.Logout)

	mux.HandleFunc("GET /", h.ServersIndex)
	mux.HandleFunc("GET /servers/new", h.RequireAdmin(h.ServerNew))
	mux.HandleFunc("POST /servers", h.RequireAdmin(h.ServerCreate))
	mux.HandleFunc("GET /servers/{id}/edit", h.ServerEdit)
	mux.HandleFunc("POST /servers/{id}", h.ServerUpdate)
	mux.HandleFunc("POST /servers/{id}/delete", h.ServerDelete)
	mux.HandleFunc("GET /servers/{id}/dump", h.ServerDumpForm)
	mux.HandleFunc("POST /servers/{id}/dump", h.ServerDump)

	mux.HandleFunc("GET /jobs", h.JobsIndex)
	mux.HandleFunc("GET /jobs/{id}", h.JobShow)
	mux.HandleFunc("GET /jobs/{id}/log", h.JobLog)
	mux.HandleFunc("POST /jobs/{id}/stop", h.JobStop)
	mux.HandleFunc("GET /jobs/{id}/files/{index}", h.JobDownload)

	mux.HandleFunc("GET /settings/ssh", h.RequireAdmin(h.SSHKeyShow))
	mux.HandleFunc("GET /settings/ssh/public-key", h.RequireAdmin(h.SSHKeyDownload))
	mux.HandleFunc("POST /settings/ssh/generate", h.RequireAdmin(h.SSHKeyGenerate))
	mux.HandleFunc("POST /settings/ssh/import", h.RequireAdmin(h.SSHKeyImport))

	mux.HandleFunc("GET /users", h.RequireAdmin(h.UsersIndex))
	mux.HandleFunc("GET /users/new", h.RequireAdmin(h.UserNew))
	mux.HandleFunc("POST /users", h.RequireAdmin(h.UserCreate))
	mux.HandleFunc("GET /users/{id}/edit", h.RequireAdmin(h.UserEdit))
	mux.HandleFunc("POST /users/{id}", h.RequireAdmin(h.UserUpdate))
	mux.HandleFunc("POST /users/{id}/delete", h.RequireAdmin(h.UserDelete))

	fmt.Printf("Server running on %s (data dir: %s)\n", listen, dataDir)
	if err := http.ListenAndServe(listen, h.RequireAuth(mux)); err != nil {
		log.Fatal(err)
	}
}
