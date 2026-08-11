package handler_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/handler"
	"github.com/mattmezza/paripari/internal/store"
	"github.com/mattmezza/paripari/internal/view"
	"github.com/mattmezza/paripari/static"
	"github.com/mattmezza/paripari/templates"
)

func newServer(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	v, err := view.New(fs.FS(templates.FS), "http://test", false)
	if err != nil {
		t.Fatal(err)
	}
	v.Context = func(r *http.Request) view.ContextData {
		s := auth.FromContext(r)
		if s == nil {
			return view.ContextData{}
		}
		return view.ContextData{User: s.User, Partner: s.Partner, Household: s.Household, CSRF: s.Token}
	}
	return handler.New(&handler.Deps{
		Store: st, View: v, Auth: auth.NewManager(st, false),
		Rates: view.Identity, BaseURL: "http://test", Static: fs.FS(static.FS),
	})
}

func post(t *testing.T, h http.Handler, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("no session cookie in response (status %d)", w.Code)
	return nil
}

func TestSignupLoginDashboard(t *testing.T) {
	h := newServer(t)

	// Anonymous dashboard redirects to the login page.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous GET /: got %d %q", w.Code, w.Header().Get("Location"))
	}

	// Login page renders.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/login", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /login: got %d: %s", w.Code, w.Body.String())
	}

	// Signup creates the household and logs the user in.
	w = post(t, h, "/signup", url.Values{
		"name": {"Elena"}, "email": {"elena@example.com"}, "password": {"paripari123"},
	}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /signup: got %d: %s", w.Code, w.Body.String())
	}
	sessionCookie(t, w)

	// Log in again and hit every page.
	w = post(t, h, "/login", url.Values{"email": {"elena@example.com"}, "password": {"paripari123"}}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /login: got %d: %s", w.Code, w.Body.String())
	}
	c := sessionCookie(t, w)

	for _, path := range []string{"/", "/income", "/expenses", "/accounts", "/transfers",
		"/net-worth", "/gold", "/goals", "/scenarios", "/trips", "/projections", "/settings"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(c)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: got %d: %s", path, w.Code, w.Body.String())
		}
	}

	// Wrong password is rejected.
	if w := post(t, h, "/login", url.Values{"email": {"elena@example.com"}, "password": {"nope"}}, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("bad password: got %d", w.Code)
	}

	// Non-GET without a CSRF token is refused.
	r := httptest.NewRequest(http.MethodPost, "/settings", nil)
	r.AddCookie(c)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("missing CSRF: got %d", w.Code)
	}
}

func TestSignupInviteFlow(t *testing.T) {
	h := newServer(t)
	w := post(t, h, "/signup", url.Values{
		"name": {"Elena"}, "email": {"a@example.com"}, "password": {"paripari123"},
	}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("first signup: %d", w.Code)
	}
	if w := post(t, h, "/signup", url.Values{
		"name": {"Marco"}, "email": {"b@example.com"}, "password": {"paripari123"}, "invite": {"NOPE1234"},
	}, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad invite should fail: got %d", w.Code)
	}
}

func TestPublicRoutes(t *testing.T) {
	h := newServer(t)
	for path, want := range map[string]int{
		"/healthz":     http.StatusOK,
		"/robots.txt":  http.StatusOK,
		"/sitemap.xml": http.StatusOK,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Errorf("GET %s: got %d want %d", path, w.Code, want)
		}
	}
}
