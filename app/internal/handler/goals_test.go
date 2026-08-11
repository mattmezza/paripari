package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A goal with a deadline gets an on-track verdict and the margin in months; one
// without gets the projected date instead, since there is nothing to be on
// track for.
func TestGoalOnTrackIndicator(t *testing.T) {
	h, _ := newServerStore(t)
	c := sessionCookie(t, post(t, h, "/signup", url.Values{
		"name": {"Elena"}, "email": {"e@example.com"}, "password": {"paripari123"},
	}, nil))
	// Income with no expenses: the whole net income is surplus, so every goal
	// has a finite ETA.
	postHX(t, h, "/income", url.Values{
		"user_id": {"1"}, "name": {"Salary"}, "kind": {"fixed"}, "pay_structure": {"12"},
		"currency": {"CHF"}, "gross_yearly": {"120000"},
	}, c)

	goal := func(name, deadline string) {
		t.Helper()
		if w := postHX(t, h, "/goals", url.Values{
			"name": {name}, "target_amount": {"100000"}, "currency": {"CHF"},
			"category": {"other"}, "deadline": {deadline},
		}, c); w.Code != http.StatusOK {
			t.Fatalf("POST /goals %s: %d %s", name, w.Code, w.Body.String())
		}
	}
	goal("Late one", time.Now().AddDate(0, -2, 0).Format("2006-01-02"))
	goal("No deadline", "")

	r := httptest.NewRequest(http.MethodGet, "/goals", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /goals: %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Behind schedule \u00b7 ", "mo late", "Reaches target ", "0% of"} {
		if !strings.Contains(body, want) {
			t.Errorf("/goals does not contain %q", want)
		}
	}
}
