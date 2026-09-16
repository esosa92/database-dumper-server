package handlers

import (
	"io"
	"net/http"
	"strings"

	"database-dumper-server/db"
)

type sshKeyData struct {
	User        db.User
	PublicKey   string
	Fingerprint string
	KeyPath     string
	Error       string
	Notice      string
}

func (h *Handler) sshKeyData(r *http.Request, errMsg, notice string) sshKeyData {
	data := sshKeyData{User: currentUser(r), KeyPath: h.keys.PrivateKeyPath(), Error: errMsg, Notice: notice}
	if pub, err := h.keys.PublicKey(); err == nil {
		data.PublicKey = pub
	}
	if fp, err := h.keys.Fingerprint(); err == nil {
		data.Fingerprint = fp
	}
	return data
}

func (h *Handler) SSHKeyShow(w http.ResponseWriter, r *http.Request) {
	h.render(w, "settings/ssh.html", h.sshKeyData(r, "", r.URL.Query().Get("notice")))
}

func (h *Handler) SSHKeyDownload(w http.ResponseWriter, r *http.Request) {
	pub, err := h.keys.PublicKey()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", "attachment; filename=\"database-dumper.pub\"")
	w.Write([]byte(pub + "\n"))
}

func (h *Handler) SSHKeyGenerate(w http.ResponseWriter, r *http.Request) {
	if err := h.keys.Generate("database-dumper"); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		h.render(w, "settings/ssh.html", h.sshKeyData(r, err.Error(), ""))
		return
	}
	http.Redirect(w, r, "/settings/ssh?notice=generated", http.StatusSeeOther)
}

func (h *Handler) SSHKeyImport(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(1 << 20)
	var pem []byte
	if f, _, err := r.FormFile("key_file"); err == nil {
		defer f.Close()
		pem, _ = io.ReadAll(io.LimitReader(f, 1<<20))
	}
	if len(strings.TrimSpace(string(pem))) == 0 {
		pem = []byte(strings.ReplaceAll(r.FormValue("key_text"), "\r\n", "\n"))
	}
	if len(strings.TrimSpace(string(pem))) == 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
		h.render(w, "settings/ssh.html", h.sshKeyData(r, "paste a private key or choose a file", ""))
		return
	}
	if !strings.HasSuffix(string(pem), "\n") {
		pem = append(pem, '\n')
	}
	if err := h.keys.Import(pem); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		h.render(w, "settings/ssh.html", h.sshKeyData(r, err.Error(), ""))
		return
	}
	http.Redirect(w, r, "/settings/ssh?notice=imported", http.StatusSeeOther)
}
