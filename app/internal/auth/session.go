package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
)

const (
	CookieName    = "pp_session"
	SessionMaxAge = 30 * 24 * time.Hour
)

// Manager wires sessions and middleware to the store.
type Manager struct {
	Store  *store.Store
	Secure bool // set from SECURE_COOKIES=1
}

func NewManager(s *store.Store, secure bool) *Manager { return &Manager{Store: s, Secure: secure} }

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// StartSession creates a session row and sets the cookie.
func (m *Manager) StartSession(w http.ResponseWriter, userID int64) error {
	token := newToken()
	exp := time.Now().UTC().Add(SessionMaxAge)
	if err := m.Store.CreateSession(token, userID, exp.Format("2006-01-02T15:04:05Z")); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", Expires: exp,
		MaxAge:   int(SessionMaxAge / time.Second),
		HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteStrictMode,
	})
	return nil
}

// EndSession deletes the session row and clears the cookie.
func (m *Manager) EndSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		m.Store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteStrictMode,
	})
}

type ctxKey int

const sessionKey ctxKey = 1

// Session is what RequireAuth puts on the request context.
type Session struct {
	Token     string
	User      *model.User
	Partner   *model.User // the other household member, if any
	Household *model.Household
}

// FromContext returns the authenticated session, or nil.
func FromContext(r *http.Request) *Session {
	s, _ := r.Context().Value(sessionKey).(*Session)
	return s
}

// load resolves the session cookie into a Session, or nil.
func (m *Manager) load(r *http.Request) *Session {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := m.Store.SessionUser(c.Value)
	if err != nil {
		return nil
	}
	hh, err := m.Store.Household(u.HouseholdID)
	if err != nil {
		return nil
	}
	s := &Session{Token: c.Value, User: u, Household: hh}
	if members, err := m.Store.UsersByHousehold(u.HouseholdID); err == nil {
		for i := range members {
			if members[i].ID != u.ID {
				s.Partner = &members[i]
				break
			}
		}
	}
	return s
}

func withSession(r *http.Request, s *Session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionKey, s))
}

// RequireAuth loads the session onto the context, enforces CSRF on non-GET
// requests and redirects anonymous visitors to /login.
func (m *Manager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := m.load(r)
		if s == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if !CheckCSRF(r, s.Token) {
			http.Error(w, "bad CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, withSession(r, s))
	})
}

// Optional loads the session if present but never blocks the request. CSRF is
// still enforced for non-GET requests from authenticated users.
func (m *Manager) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := m.load(r)
		if s == nil {
			next.ServeHTTP(w, r)
			return
		}
		if !CheckCSRF(r, s.Token) {
			http.Error(w, "bad CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, withSession(r, s))
	})
}

// CheckCSRF passes safe methods, then either an htmx request from the same
// origin, or a matching `_csrf` form field.
func CheckCSRF(r *http.Request, token string) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	// htmx sends the token as a header (hx-headers on <body>).
	if token != "" && r.Header.Get("X-CSRF-Token") == token {
		return true
	}
	if r.Header.Get("HX-Request") == "true" {
		switch r.Header.Get("Sec-Fetch-Site") {
		case "same-origin", "none":
			return true
		case "":
			// Browser did not send fetch metadata: fall through to the token check.
		default:
			return false
		}
	}
	if token == "" {
		return false
	}
	// The design system's forms use csrf_token; older ones use _csrf.
	return r.PostFormValue("_csrf") == token || r.PostFormValue("csrf_token") == token
}
