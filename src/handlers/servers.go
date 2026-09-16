package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"database-dumper-server/db"
)

type serverRow struct {
	db.Server
	CanEdit bool
}

type indexData struct {
	User    db.User
	Servers []serverRow
	Query   string
}

func (h *Handler) ServersIndex(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	q := r.URL.Query().Get("q")
	servers, err := db.SearchServersFor(q, u)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := indexData{User: u, Query: q}
	for _, s := range servers {
		data.Servers = append(data.Servers, serverRow{Server: s, CanEdit: canEdit(u, s.ID)})
	}

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, "servers/rows.html", data)
		return
	}
	h.render(w, "servers/index.html", data)
}

func (h *Handler) ServerNew(w http.ResponseWriter, r *http.Request) {
	h.render(w, "servers/form.html", formData{User: currentUser(r), Server: db.Server{Enabled: true, SSHPort: 22}})
}

func (h *Handler) ServerCreate(w http.ResponseWriter, r *http.Request) {
	s := serverFromForm(r)
	if err := db.InsertServer(s); err != nil {
		h.renderFormError(w, r, s, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) loadServer(w http.ResponseWriter, r *http.Request, check func(db.User, string) bool) (db.Server, bool) {
	id := r.PathValue("id")
	if !check(currentUser(r), id) {
		h.forbidden(w)
		return db.Server{}, false
	}
	s, err := db.GetServerByID(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return s, false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return s, false
	}
	return s, true
}

func (h *Handler) ServerEdit(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadServer(w, r, canView)
	if !ok {
		return
	}
	h.render(w, "servers/form.html", formData{User: currentUser(r), Server: s, ReadOnly: !canEdit(currentUser(r), s.ID)})
}

func (h *Handler) ServerUpdate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadServer(w, r, canEdit); !ok {
		return
	}
	s := serverFromForm(r)
	s.ID = r.PathValue("id")
	if err := db.UpdateServer(s); err != nil {
		h.renderFormError(w, r, s, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) ServerDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadServer(w, r, canEdit); !ok {
		return
	}
	if err := db.DeleteServer(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func serverFromForm(r *http.Request) db.Server {
	r.ParseForm()

	port, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("ssh_port")))
	if port <= 0 {
		port = 22
	}

	return db.Server{
		ID:                     strings.TrimSpace(r.FormValue("id")),
		SSHHost:                strings.TrimSpace(r.FormValue("ssh_host")),
		SSHPort:                port,
		SSHPass:                r.FormValue("ssh_pass"),
		RemoteEnvPath:          strings.TrimSpace(r.FormValue("remote_env_path")),
		LocalPath:              strings.TrimSpace(r.FormValue("local_path")),
		Enabled:                r.FormValue("enabled") == "on",
		WithCoreConfig:         r.FormValue("with_core_config") == "on",
		OnlyCoreConfig:         r.FormValue("only_core_config") == "on",
		EnableSetGTIDPurgedOff: r.FormValue("enable_set_gtid_purged_off") == "on",
		IgnoreTables:           lines(r.FormValue("ignore_tables")),
		OnlyTables:             strings.TrimSpace(strings.ReplaceAll(r.FormValue("only_tables"), "\r", "")),
		NetBufferLength:        strings.TrimSpace(r.FormValue("net_buffer_length")),
		SkipExtendedInsert:     r.FormValue("skip_extended_insert") == "on",
		SkipAddLocks:           r.FormValue("skip_add_locks") == "on",
		SkipDisableKeys:        r.FormValue("skip_disable_keys") == "on",
		SkipLockTables:         r.FormValue("skip_lock_tables") == "on",
		SkipAddDropTable:       r.FormValue("skip_add_drop_table") == "on",
		SingleTableMode:        r.FormValue("single_table_mode") == "on",
		DumpClient:             strings.TrimSpace(r.FormValue("dump_client")),
	}
}

type formData struct {
	User     db.User
	Server   db.Server
	ReadOnly bool
	Error    string
}

func (h *Handler) renderFormError(w http.ResponseWriter, r *http.Request, s db.Server, err error) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	h.render(w, "servers/form.html", formData{User: currentUser(r), Server: s, Error: err.Error()})
}

func lines(raw string) []string {
	out := []string{}
	for _, l := range strings.Split(raw, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
