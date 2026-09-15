package handlers

import (
	"context"
	"net/http"
	"strings"

	"database-dumper-server/db"
)

type ctxKey int

const userKey ctxKey = 0

const sessionCookie = "dumper_session"

func currentUser(r *http.Request) db.User {
	u, _ := r.Context().Value(userKey).(db.User)
	return u
}

func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		u, err := db.GetSessionUser(c.Value)
		if err != nil {
			clearSession(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

type loginData struct {
	User     db.User
	Username string
	Error    string
}

func (h *Handler) LoginForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, "auth/login.html", loginData{})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	u, err := db.Authenticate(username, r.FormValue("password"))
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		h.render(w, "auth/login.html", loginData{Username: username, Error: err.Error()})
		return
	}
	token, err := db.CreateSession(u.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		db.DeleteSession(c.Value)
	}
	clearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func roleFor(u db.User, serverID string) string {
	if u.IsAdmin {
		return db.RoleEditor
	}
	role, err := db.ServerRole(u.ID, serverID)
	if err != nil {
		return ""
	}
	return role
}

func canView(u db.User, serverID string) bool {
	return roleFor(u, serverID) != ""
}

func canEdit(u db.User, serverID string) bool {
	return roleFor(u, serverID) == db.RoleEditor
}

func (h *Handler) forbidden(w http.ResponseWriter) {
	http.Error(w, "forbidden", http.StatusForbidden)
}

func (h *Handler) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin {
			h.forbidden(w)
			return
		}
		next(w, r)
	}
}
