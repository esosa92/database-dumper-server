package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"database-dumper-server/db"
)

type usersIndexData struct {
	User  db.User
	Users []db.User
}

func (h *Handler) UsersIndex(w http.ResponseWriter, r *http.Request) {
	users, err := db.ListUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, "users/index.html", usersIndexData{User: currentUser(r), Users: users})
}

type serverAccessRow struct {
	ServerID string
	Role     string
}

type userFormData struct {
	User    db.User
	Target  db.User
	Servers []serverAccessRow
	Error   string
}

func (h *Handler) userForm(r *http.Request, target db.User, access []db.ServerAccess) (userFormData, error) {
	servers, err := db.GetAllServers()
	if err != nil {
		return userFormData{}, err
	}
	roles := map[string]string{}
	for _, a := range access {
		roles[a.ServerID] = a.Role
	}
	data := userFormData{User: currentUser(r), Target: target}
	for _, s := range servers {
		data.Servers = append(data.Servers, serverAccessRow{ServerID: s.ID, Role: roles[s.ID]})
	}
	return data, nil
}

func (h *Handler) renderUserForm(w http.ResponseWriter, r *http.Request, target db.User, access []db.ServerAccess, formErr error) {
	data, err := h.userForm(r, target, access)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if formErr != nil {
		data.Error = formErr.Error()
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	h.render(w, "users/form.html", data)
}

func (h *Handler) UserNew(w http.ResponseWriter, r *http.Request) {
	h.renderUserForm(w, r, db.User{}, nil, nil)
}

func accessFromForm(r *http.Request) []db.ServerAccess {
	r.ParseForm()
	var out []db.ServerAccess
	for key, vals := range r.Form {
		if len(key) > 5 && key[:5] == "role_" && len(vals) > 0 && vals[0] != "" {
			out = append(out, db.ServerAccess{ServerID: key[5:], Role: vals[0]})
		}
	}
	return out
}

func (h *Handler) UserCreate(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	target := db.User{Username: r.FormValue("username"), IsAdmin: r.FormValue("is_admin") == "on"}
	access := accessFromForm(r)

	u, err := db.CreateUser(target.Username, r.FormValue("password"), target.IsAdmin)
	if err != nil {
		h.renderUserForm(w, r, target, access, err)
		return
	}
	if !u.IsAdmin {
		if err := db.SetUserServers(u.ID, access); err != nil {
			h.renderUserForm(w, r, u, access, err)
			return
		}
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (h *Handler) loadUser(w http.ResponseWriter, r *http.Request) (db.User, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.User{}, false
	}
	u, err := db.GetUserByID(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return u, false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return u, false
	}
	return u, true
}

func (h *Handler) UserEdit(w http.ResponseWriter, r *http.Request) {
	u, ok := h.loadUser(w, r)
	if !ok {
		return
	}
	access, err := db.GetUserServers(u.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderUserForm(w, r, u, access, nil)
}

func (h *Handler) UserUpdate(w http.ResponseWriter, r *http.Request) {
	u, ok := h.loadUser(w, r)
	if !ok {
		return
	}
	r.ParseForm()
	target := db.User{ID: u.ID, Username: r.FormValue("username"), IsAdmin: r.FormValue("is_admin") == "on"}
	access := accessFromForm(r)

	if u.ID == currentUser(r).ID && !target.IsAdmin {
		h.renderUserForm(w, r, target, access, errors.New("you cannot remove your own admin flag"))
		return
	}
	if err := db.UpdateUser(u.ID, target.Username, target.IsAdmin); err != nil {
		h.renderUserForm(w, r, target, access, err)
		return
	}
	if pw := r.FormValue("password"); pw != "" {
		if err := db.SetPassword(u.ID, pw); err != nil {
			h.renderUserForm(w, r, target, access, err)
			return
		}
	}
	if target.IsAdmin {
		access = nil
	}
	if err := db.SetUserServers(u.ID, access); err != nil {
		h.renderUserForm(w, r, target, access, err)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (h *Handler) UserDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := h.loadUser(w, r)
	if !ok {
		return
	}
	if u.ID == currentUser(r).ID {
		http.Error(w, "you cannot delete yourself", http.StatusBadRequest)
		return
	}
	if err := db.DeleteUser(u.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
