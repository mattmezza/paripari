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
	CookieName = "pp_session"
	// PendingCookieName is the short-lived half-login cookie set when a user
	// with 2FA enabled has passed the password step and must still enter a code.
	PendingCookieName = "pp_2fa"

	// DefaultSessionMaxAge is what new users get (30 days), and the lifetime
	// used when a session must be renewed but its owner has no preference.
	DefaultSessionMaxAge = 30 * 24 * time.Hour

	// tsLayout matches store.now(): ISO8601 UTC, second precision, "Z" suffix.
	tsLayout = "2006-01-02T15:04:05Z"

	// neverCookieMaxAge is how long a "never expire" session cookie lives in
	// the browser. The DB row has no expiry at all; the cookie still needs a
	// concrete Max-Age, and 10 years is as close to "never" as browsers get.
	neverCookieMaxAge = 10 * 365 * 24 * time.Hour

	// pendingLoginTTL is how long a password-verified half-login stays valid
	// while the TOTP / recovery code is entered.
	pendingLoginTTL = 5 * time.Minute

	// StepUpWindow is how long a successful step-up re-auth stays valid.
	StepUpWindow = 10 * time.Minute
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

// SessionLifetime returns the user's preferred session lifetime, or 0 for
// "never expire". The database is the source of truth: a NULL
// session_expiry_days (model nil) means never; the store always stamps new
// users with the 30-day default.
func SessionLifetime(u *model.User) time.Duration {
	if u == nil {
		return DefaultSessionMaxAge
	}
	if u.SessionExpiryDays == nil {
		return 0 // never
	}
	d := time.Duration(*u.SessionExpiryDays) * 24 * time.Hour
	if d <= 0 {
		return 0
	}
	return d
}

// expiryStamp formats a lifetime expiry for the sessions row; nil means never.
func expiryStamp(lifetime time.Duration) *string {
	if lifetime <= 0 {
		return nil
	}
	s := time.Now().UTC().Add(lifetime).Format(tsLayout)
	return &s
}

// setSessionCookie writes the pp_session cookie. exp nil (never) gets a
// long-lived cookie instead of an unlimited one.
func (m *Manager) setSessionCookie(w http.ResponseWriter, token string, lifetime time.Duration, exp *time.Time) {
	c := &http.Cookie{
		Name: CookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteStrictMode,
	}
	if exp != nil {
		c.Expires = *exp
		c.MaxAge = int(lifetime / time.Second)
	} else {
		c.MaxAge = int(neverCookieMaxAge / time.Second)
		c.Expires = time.Now().UTC().Add(neverCookieMaxAge)
	}
	http.SetCookie(w, c)
}

// StartSession creates a session row honouring the user's lifetime preference
// and sets the cookie.
func (m *Manager) StartSession(w http.ResponseWriter, u *model.User) error {
	token := newToken()
	lifetime := SessionLifetime(u)
	if err := m.Store.CreateSession(token, u.ID, expiryStamp(lifetime)); err != nil {
		return err
	}
	if lifetime > 0 {
		exp := time.Now().UTC().Add(lifetime)
		m.setSessionCookie(w, token, lifetime, &exp)
	} else {
		m.setSessionCookie(w, token, 0, nil)
	}
	return nil
}

// RefreshSession re-stamps the current session with the user's current
// lifetime preference and updates the cookie — used when the setting changes,
// so it takes effect immediately rather than on next login.
func (m *Manager) RefreshSession(w http.ResponseWriter, token string, u *model.User) error {
	lifetime := SessionLifetime(u)
	if err := m.Store.UpdateSessionExpiry(token, expiryStamp(lifetime)); err != nil {
		return err
	}
	if lifetime > 0 {
		exp := time.Now().UTC().Add(lifetime)
		m.setSessionCookie(w, token, lifetime, &exp)
	} else {
		m.setSessionCookie(w, token, 0, nil)
	}
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
	// ExpiresAt is the session row's expiry ("" = never); VerifiedAt is the
	// last step-up re-auth ("" = none yet). Both are ISO8601 UTC strings.
	ExpiresAt  string
	VerifiedAt string
}

// FromContext returns the authenticated session, or nil.
func FromContext(r *http.Request) *Session {
	s, _ := r.Context().Value(sessionKey).(*Session)
	return s
}

// RequiresStepUp reports whether the session needs a fresh step-up re-auth
// (password + TOTP) before sensitive actions.
func RequiresStepUp(s *Session) bool {
	if s == nil || s.User == nil {
		return true
	}
	if s.VerifiedAt == "" {
		return true
	}
	t, err := time.Parse(tsLayout, s.VerifiedAt)
	if err != nil {
		return true
	}
	return time.Since(t) > StepUpWindow
}

// renewal carries the outcome of the sliding-window check; the middleware
// applies it (DB row + cookie) only after the request passes CSRF.
type renewal struct {
	expires  *time.Time // nil = never (DB row set to NULL)
	lifetime time.Duration
}

// maybeRenew decides whether an active session is close to its expiry and
// should be extended. Sessions with more than half their row lifetime left
// are left alone; the user's current preference is the renewal target.
func (m *Manager) maybeRenew(u *model.User, sess *model.Session) *renewal {
	if sess == nil || sess.ExpiresAt == "" {
		return nil // never-expiring: nothing to slide
	}
	exp, err := time.Parse(tsLayout, sess.ExpiresAt)
	if err != nil {
		return nil
	}
	lifetime := SessionLifetime(u)
	remaining := time.Until(exp)
	if lifetime > 0 && remaining > lifetime/2 {
		return nil
	}
	// Extend: expired-ish, or the user moved to "never".
	if lifetime == 0 {
		return &renewal{expires: nil, lifetime: 0}
	}
	newExp := time.Now().UTC().Add(lifetime)
	return &renewal{expires: &newExp, lifetime: lifetime}
}

// load resolves the session cookie into a Session, or nil.
func (m *Manager) load(r *http.Request) (*Session, *renewal) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	u, sess, err := m.Store.SessionUser(c.Value)
	if err != nil {
		return nil, nil
	}
	hh, err := m.Store.Household(u.HouseholdID)
	if err != nil {
		return nil, nil
	}
	s := &Session{Token: c.Value, User: u, Household: hh,
		ExpiresAt: sess.ExpiresAt, VerifiedAt: sess.VerifiedAt}
	if members, err := m.Store.UsersByHousehold(u.HouseholdID); err == nil {
		for i := range members {
			if members[i].ID != u.ID {
				s.Partner = &members[i]
				break
			}
		}
	}
	return s, m.maybeRenew(u, sess)
}

func withSession(r *http.Request, s *Session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionKey, s))
}

// RequireAuth loads the session onto the context, enforces CSRF on non-GET
// requests and redirects anonymous visitors to /login.
func (m *Manager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ren := m.load(r)
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
		if ren != nil {
			m.applyRenewal(w, s.Token, ren)
		}
		next.ServeHTTP(w, withSession(r, s))
	})
}

// applyRenewal writes the extended expiry to the DB row and mirrors it onto
// the client cookie. Only called after the request passed CSRF.
func (m *Manager) applyRenewal(w http.ResponseWriter, token string, ren *renewal) {
	if ren.expires != nil {
		stamp := ren.expires.UTC().Format(tsLayout)
		if err := m.Store.UpdateSessionExpiry(token, &stamp); err != nil {
			return
		}
		m.setSessionCookie(w, token, ren.lifetime, ren.expires)
		return
	}
	if err := m.Store.UpdateSessionExpiry(token, nil); err != nil {
		return
	}
	m.setSessionCookie(w, token, 0, nil)
}

// Optional loads the session if present but never blocks the request. CSRF is
// still enforced for non-GET requests from authenticated users.
func (m *Manager) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ren := m.load(r)
		if s == nil {
			next.ServeHTTP(w, r)
			return
		}
		if !CheckCSRF(r, s.Token) {
			http.Error(w, "bad CSRF token", http.StatusForbidden)
			return
		}
		if ren != nil {
			m.applyRenewal(w, s.Token, ren)
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

// StartPendingLogin begins the 2FA half-login: password verified, code not
// yet. Returns the pending token and its cookie.
func (m *Manager) StartPendingLogin(w http.ResponseWriter, userID int64) error {
	token := newToken()
	exp := time.Now().UTC().Add(pendingLoginTTL)
	if err := m.Store.CreatePendingLogin(token, userID, exp.Format(tsLayout)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: PendingCookieName, Value: token, Path: "/",
		Expires: exp, MaxAge: int(pendingLoginTTL / time.Second),
		HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteStrictMode,
	})
	return nil
}

// PendingUser resolves the pending-login cookie to its user, if fresh.
func (m *Manager) PendingUser(r *http.Request) *model.User {
	c, err := r.Cookie(PendingCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := m.Store.PendingLoginUser(c.Value)
	if err != nil {
		return nil
	}
	return u
}

// FinishPendingLogin consumes the half-login and starts the real session.
func (m *Manager) FinishPendingLogin(w http.ResponseWriter, r *http.Request, u *model.User) error {
	if c, err := r.Cookie(PendingCookieName); err == nil {
		m.Store.DeletePendingLogin(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: PendingCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteStrictMode,
	})
	return m.StartSession(w, u)
}

// MarkStepUp stamps the session as freshly step-up verified.
func (m *Manager) MarkStepUp(w http.ResponseWriter, r *http.Request) error {
	return m.Store.UpdateSessionVerifiedAt(FromContext(r).Token, time.Now().UTC().Format(tsLayout))
}
