package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHistoryRoutes(t *testing.T) {
	h := newServer(t)
	w := post(t, h, "/signup", url.Values{
		"name": {"Elena"}, "email": {"hist@example.com"}, "password": {"paripari123"},
	}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("signup: %d", w.Code)
	}
	c := sessionCookie(t, w)

	for _, path := range []string{"/history", "/history?range=3m", "/history?range=all",
		"/history?range=bogus", "/history.csv", "/history.csv?range=all"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(c)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: got %d: %s", path, w.Code, w.Body.String())
		}
		if strings.HasPrefix(path, "/history.csv") &&
			!strings.HasPrefix(w.Body.String(), "date,currency,net_worth_total") {
			t.Errorf("GET %s: unexpected CSV header %q", path, w.Body.String())
		}
	}
}
