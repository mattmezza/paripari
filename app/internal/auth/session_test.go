package auth_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

func signupUser(t *testing.T, st *store.Store, email string) *model.User {
	t.Helper()
	u, err := auth.Signup(st, "Tester", email, "paripari123", "")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func intPtr(n int) *int { return &n }

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in response (status %d)", auth.CookieName, w.Code)
	return nil
}

// rowExpiry reads the single session row's expires_at ("" = NULL).
func rowExpiry(t *testing.T, st *store.Store, token string) string {
	t.Helper()
	var e *string
	if err := st.DB.QueryRow(`SELECT expires_at FROM sessions WHERE token = ?`, token).Scan(&e); err != nil {
		t.Fatal(err)
	}
	if e == nil {
		return ""
	}
	return *e
}

func TestStartSessionDefaultLifetime(t *testing.T) {
	st := newStore(t)
	u := signupUser(t, st, "a@example.com")
	m := auth.NewManager(st, false)
	w := httptest.NewRecorder()
	if err := m.StartSession(w, u); err != nil {
		t.Fatal(err)
	}
	c := sessionCookie(t, w)
	if c.MaxAge != int(30*24*time.Hour/time.Second) {
		t.Errorf("cookie Max-Age = %d, want %d", c.MaxAge, int(30*24*time.Hour/time.Second))
	}
	exp, err := time.Parse("2006-01-02T15:04:05Z", rowExpiry(t, st, c.Value))
	if err != nil {
		t.Fatalf("expires_at not parseable: %v", err)
	}
	// ≈ now + 30 days.
	if d := time.Until(exp); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Errorf("expires in %v, want ~30 days", d)
	}
}

func TestStartSessionNeverExpires(t *testing.T) {
	st := newStore(t)
	u := signupUser(t, st, "b@example.com")
	u.SessionExpiryDays = nil // NULL preference = never
	m := auth.NewManager(st, false)
	w := httptest.NewRecorder()
	if err := m.StartSession(w, u); err != nil {
		t.Fatal(err)
	}
	c := sessionCookie(t, w)
	// Long-lived cookie, but a far shorter DB lifetime is impossible: NULL row.
	if got := rowExpiry(t, st, c.Value); got != "" {
		t.Errorf("expires_at = %q, want NULL", got)
	}
	if c.MaxAge < 5*365*24*3600 {
		t.Errorf("never-session cookie Max-Age = %d, want years", c.MaxAge)
	}
	// The session resolves past any wall-clock deadline.
	u2, sess, err := st.SessionUser(c.Value)
	if err != nil {
		t.Fatalf("SessionUser: %v", err)
	}
	if u2.ID != u.ID || sess.ExpiresAt != "" {
		t.Errorf("SessionUser = %+v / %+v", u2, sess)
	}
}

func TestDeleteExpiredSessionsSkipsNever(t *testing.T) {
	st := newStore(t)
	u := signupUser(t, st, "c@example.com")
	past := time.Now().Add(-time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	if err := st.CreateSession("expired-token", u.ID, &past); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession("never-token", u.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteExpiredSessions(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SessionUser("expired-token"); err == nil {
		t.Error("expired session survived GC")
	}
	if _, _, err := st.SessionUser("never-token"); err != nil {
		t.Errorf("never-expiring session was GC'd: %v", err)
	}
}

func TestSlidingRenewal(t *testing.T) {
	st := newStore(t)
	u := signupUser(t, st, "d@example.com")
	u.SessionExpiryDays = intPtr(30)
	m := auth.NewManager(st, false)

	// Seed a session with only 10 of its 30 days left: past the half-life, so
	// an authenticated request must slide it back to a full lifetime.
	soon := time.Now().UTC().Add(10 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	if err := st.CreateSession("renew-me", u.ID, &soon); err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "renew-me"})
	w := httptest.NewRecorder()
	m.RequireAuth(inner).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("request: %d", w.Code)
	}
	exp, err := time.Parse("2006-01-02T15:04:05Z", rowExpiry(t, st, "renew-me"))
	if err != nil {
		t.Fatalf("renewed expires_at not parseable: %v", err)
	}
	if d := time.Until(exp); d < 29*24*time.Hour {
		t.Errorf("session not renewed: expires in %v, want ~30 days", d)
	}
	// The response must carry the extended cookie.
	if c := sessionCookie(t, w); c.MaxAge != int(30*24*time.Hour/time.Second) {
		t.Errorf("renewed cookie Max-Age = %d", c.MaxAge)
	}
}

func TestNoRenewalWithinHalfLife(t *testing.T) {
	st := newStore(t)
	u := signupUser(t, st, "e@example.com")
	u.SessionExpiryDays = intPtr(30)
	far := time.Now().UTC().Add(25 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	if err := st.CreateSession("fresh-enough", u.ID, &far); err != nil {
		t.Fatal(err)
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "fresh-enough"})
	w := httptest.NewRecorder()
	m := auth.NewManager(st, false)
	m.RequireAuth(inner).ServeHTTP(w, req)
	if got := rowExpiry(t, st, "fresh-enough"); got != far {
		t.Errorf("expires_at changed to %q, want %q (no renewal before half-life)", got, far)
	}
	if strings.Contains(w.Body.String(), "Set-Cookie") {
		// A renewal would have written a fresh Set-Cookie header.
		if got := w.Result().Header.Get("Set-Cookie"); got != "" {
			t.Errorf("unexpected Set-Cookie on non-renewed request: %s", got)
		}
	}
}

func TestPreferenceChangeAppliesImmediately(t *testing.T) {
	st := newStore(t)
	u := signupUser(t, st, "f@example.com")
	m := auth.NewManager(st, false)

	w0 := httptest.NewRecorder()
	if err := m.StartSession(w0, u); err != nil {
		t.Fatal(err)
	}
	token := sessionCookie(t, w0).Value

	u.SessionExpiryDays = intPtr(7)
	w := httptest.NewRecorder()
	if err := m.RefreshSession(w, token, u); err != nil {
		t.Fatal(err)
	}
	c := sessionCookie(t, w)
	if c.MaxAge != int(7*24*time.Hour/time.Second) {
		t.Errorf("cookie Max-Age = %d, want %d", c.MaxAge, int(7*24*time.Hour/time.Second))
	}
	exp, err := time.Parse("2006-01-02T15:04:05Z", rowExpiry(t, st, token))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(exp); d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Errorf("expires in %v, want ~7 days", d)
	}

	// Moving to "never" clears the row expiry.
	u.SessionExpiryDays = nil
	w2 := httptest.NewRecorder()
	if err := m.RefreshSession(w2, token, u); err != nil {
		t.Fatal(err)
	}
	if got := rowExpiry(t, st, token); got != "" {
		t.Errorf("expires_at = %q after switching to never, want NULL", got)
	}
}

func TestRequiresStepUp(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	stale := time.Now().Add(-auth.StepUpWindow - time.Minute).UTC().Format("2006-01-02T15:04:05Z")
	for _, tc := range []struct {
		name string
		s    *auth.Session
		want bool
	}{
		{"no session", nil, true},
		{"never verified", &auth.Session{}, true},
		{"freshly verified", &auth.Session{User: &model.User{}, VerifiedAt: now}, false},
		{"stale verification", &auth.Session{User: &model.User{}, VerifiedAt: stale}, true},
	} {
		if got := auth.RequiresStepUp(tc.s); got != tc.want {
			t.Errorf("%s: RequiresStepUp = %v, want %v", tc.name, got, tc.want)
		}
	}
}
