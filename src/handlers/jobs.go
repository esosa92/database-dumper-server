package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"database-dumper-server/db"
	"database-dumper-server/dumper"
)

type dumpFormData struct {
	User   db.User
	Server db.Server
	Parts  []dumper.Partition
	Error  string
}

func (h *Handler) ServerDumpForm(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadServer(w, r, canView)
	if !ok {
		return
	}
	h.render(w, "jobs/new.html", dumpFormData{User: currentUser(r), Server: s, Parts: dumper.ParsePartitions(s.OnlyTables)})
}

func (h *Handler) ServerDump(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadServer(w, r, canView)
	if !ok {
		return
	}

	r.ParseForm()
	opts := dumper.Options{
		Tables:         splitList(r.FormValue("tables")),
		OnlyCoreConfig: r.FormValue("only_core_config") == "on",
		Part:           strings.TrimSpace(r.FormValue("part")),
	}

	job, err := h.jobs.Start(s, opts)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		h.render(w, "jobs/new.html", dumpFormData{User: currentUser(r), Server: s, Parts: dumper.ParsePartitions(s.OnlyTables), Error: err.Error()})
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/jobs/%d", job.ID), http.StatusSeeOther)
}

func splitList(raw string) []string {
	var out []string
	for _, t := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

type jobData struct {
	User  db.User
	Job   dumper.Snapshot
	Files []jobFile
}

type jobFile struct {
	Index  int
	Name   string
	Exists bool
}

func (h *Handler) jobData(r *http.Request, snap dumper.Snapshot) jobData {
	data := jobData{User: currentUser(r), Job: snap}
	for i, f := range snap.Files {
		_, err := os.Stat(f)
		data.Files = append(data.Files, jobFile{Index: i, Name: filepath.Base(f), Exists: err == nil})
	}
	return data
}

type jobsIndexData struct {
	User db.User
	Jobs []dumper.Snapshot
}

func (h *Handler) JobsIndex(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	jobs, err := h.jobs.List(u, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, "jobs/index.html", jobsIndexData{User: u, Jobs: jobs})
}

func (h *Handler) loadJob(w http.ResponseWriter, r *http.Request) (dumper.Snapshot, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return dumper.Snapshot{}, false
	}
	snap, ok := h.jobs.Get(id)
	if !ok {
		http.NotFound(w, r)
		return snap, false
	}
	if !canView(currentUser(r), snap.ServerID) {
		h.forbidden(w)
		return snap, false
	}
	return snap, true
}

func (h *Handler) JobShow(w http.ResponseWriter, r *http.Request) {
	if snap, ok := h.loadJob(w, r); ok {
		h.render(w, "jobs/show.html", h.jobData(r, snap))
	}
}

func (h *Handler) JobLog(w http.ResponseWriter, r *http.Request) {
	if snap, ok := h.loadJob(w, r); ok {
		h.render(w, "jobs/log.html", h.jobData(r, snap))
	}
}

func (h *Handler) JobStop(w http.ResponseWriter, r *http.Request) {
	snap, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	if job, running := h.jobs.Running(snap.ID); running {
		job.Stop()
		snap = job.Snapshot()
	}
	h.render(w, "jobs/log.html", h.jobData(r, snap))
}

func (h *Handler) JobDownload(w http.ResponseWriter, r *http.Request) {
	snap, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || idx < 0 || idx >= len(snap.Files) {
		http.NotFound(w, r)
		return
	}
	path := snap.Files[idx]
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "file no longer exists on disk", http.StatusGone)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(path)+"\"")
	w.Header().Set("Content-Type", "application/gzip")
	http.ServeFile(w, r, path)
}
