package handler

import (
	"net/http"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/view"
)

// authForm is the Data passed to the auth templates.
type authForm struct {
	Error  string
	Email  string
	Name   string
	Invite string
}

func registerAuth(mux *http.ServeMux, d *Deps) {
	mux.Handle("GET /login", d.Auth.Optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.FromContext(r) != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		d.View.Render(w, r, "auth-login", &view.PageData{Title: "Log in", Data: &authForm{}})
	})))

	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		email, password := r.PostFormValue("email"), r.PostFormValue("password")
		u, err := auth.Login(d.Store, email, password)
		if err != nil {
			d.View.RenderStatus(w, r, http.StatusUnauthorized, "auth-login",
				&view.PageData{Title: "Log in", Data: &authForm{Error: err.Error(), Email: email}})
			return
		}
		if err := d.Auth.StartSession(w, u.ID); err != nil {
			http.Error(w, "could not start session", http.StatusInternalServerError)
			return
		}
		redirect(w, r, "/")
	})

	mux.Handle("GET /signup", d.Auth.Optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.FromContext(r) != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		d.View.Render(w, r, "auth-signup", &view.PageData{
			Title: "Sign up", Data: &authForm{Invite: r.URL.Query().Get("invite")}})
	})))

	mux.HandleFunc("POST /signup", func(w http.ResponseWriter, r *http.Request) {
		f := &authForm{
			Name:   r.PostFormValue("name"),
			Email:  r.PostFormValue("email"),
			Invite: r.PostFormValue("invite"),
		}
		u, err := auth.Signup(d.Store, f.Name, f.Email, r.PostFormValue("password"), f.Invite)
		if err != nil {
			f.Error = err.Error()
			d.View.RenderStatus(w, r, http.StatusBadRequest, "auth-signup",
				&view.PageData{Title: "Sign up", Data: f})
			return
		}
		if err := d.Auth.StartSession(w, u.ID); err != nil {
			http.Error(w, "could not start session", http.StatusInternalServerError)
			return
		}
		view.SetFlash(w, "Welcome to PariPari.")
		redirect(w, r, "/")
	})

	// Logout is CSRF-checked by Optional (it carries a session).
	mux.Handle("POST /logout", d.Auth.Optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.Auth.EndSession(w, r)
		redirect(w, r, "/login")
	})))
}

// redirect sends an htmx-aware redirect.
func redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}
