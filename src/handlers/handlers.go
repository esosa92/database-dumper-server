package handlers

import (
	"html/template"
	"net/http"

	"database-dumper-server/dumper"
)

type Handler struct {
	tmpl *template.Template
	jobs *dumper.Manager
}

func New(tmpl *template.Template, jobs *dumper.Manager) *Handler {
	return &Handler{tmpl: tmpl, jobs: jobs}
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
