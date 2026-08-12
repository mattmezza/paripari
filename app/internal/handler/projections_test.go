package handler_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// get fetches a page as a browser would, with the session cookie.
func get(t *testing.T, h http.Handler, path string, c *http.Cookie) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d: %s", path, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// The knobs the page encodes in its query string, posted the way the Save
// button does: hx-include serialises #projection-form alongside the name.
func projectionForm(name, rate, horizon string) url.Values {
	return url.Values{
		"name": {name}, "rate": {rate}, "horizon": {horizon},
		"inflation": {"0.02"}, "oneoff_at": {"12"}, "oneoff_amount": {"1000"},
		"growth_a": {"0.03"}, "growth_b": {"0.02"},
		"promo_at": {"24"}, "promo_pct": {"0.0800"}, "promo_who": {"a"},
	}
}

func TestSavedProjectionsCRUD(t *testing.T) {
	h, st := newServerStore(t)
	c, hh := signup(t, h, st, "p@example.com")

	// --- create ---
	w := postHX(t, h, "/projections/saved", projectionForm("Cautious", "0.03", "5"), c)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /projections/saved: %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Cautious") ||
		!strings.Contains(body, "3.0% · 5 years · 2.0% inflation · 1 one-off · 3.0% income growth · 1 raise") {
		t.Errorf("saved row does not summarise its assumptions: %s", body)
	}

	// A name is required, and a rejected save writes nothing.
	if w := postHX(t, h, "/projections/saved", projectionForm("", "0.03", "5"), c); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("POST with no name: %d, want 422", w.Code)
	}

	// --- list ---
	saved, err := st.SavedProjections(hh)
	if err != nil || len(saved) != 1 {
		t.Fatalf("stored rows = %+v (%v)", saved, err)
	}
	page := get(t, h, "/projections", c)
	if !strings.Contains(page, "Cautious") {
		t.Errorf("/projections does not list the saved projection")
	}
	loadURL := html.UnescapeString(between(t, page, `<a class="link" href="`, `">Cautious`))
	if loadURL != "/projections?"+saved[0].Params {
		t.Fatalf("load link = %q, want %q", loadURL, "/projections?"+saved[0].Params)
	}

	// --- load: the saved params drive the page they came from ---
	loaded := get(t, h, loadURL, c)
	for _, want := range []string{
		`>3.0%</span>`, // return-rate readout
		`value="5" style="accent-color:var(--color-accent);margin-inline-end:.375rem" checked`, // 5y horizon
		`>2.0%</span>`, // inflation readout
		`{"At":12,"Cents":100000,"AmountStr":"1000.00"}`,                 // the one-off row
		`id="growth-a" min="-0.10" max="0.10" step="0.001" value="0.03"`, // income growth slider
		`{"At":24,"Pct":0.08,"Percent":8,"Who":"a"}`,                     // the promotion row
	} {
		if !strings.Contains(loaded, want) {
			t.Errorf("loaded projection is missing %q", want)
		}
	}

	// A solo household has no partner B, so a raise aimed at one is dropped
	// rather than quietly landing on partner A.
	if solo := get(t, h, loadURL+"&promo_at=6&promo_pct=0.1&promo_who=b", c); strings.Contains(solo, `"Who":"b"`) {
		t.Error("promotion for a partner the household does not have was kept")
	}

	// --- update: renames and restates the assumptions in one go ---
	w = do(t, h, http.MethodPut, "/projections/saved/"+itoa(saved[0].ID), projectionForm("Bold", "0.07", "20"), c)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Bold") ||
		!strings.Contains(body, "7.0% · 20 years") || strings.Contains(body, ">Cautious<") {
		t.Errorf("update did not replace name and assumptions: %s", body)
	}
	if after, _ := st.SavedProjections(hh); len(after) != 1 || !strings.Contains(after[0].Params, "horizon=20") {
		t.Errorf("stored rows after update = %+v", after)
	}

	// --- delete ---
	w = do(t, h, http.MethodDelete, "/projections/saved/"+itoa(saved[0].ID), nil, c)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE: %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Nothing saved yet") {
		t.Errorf("empty state not rendered after delete: %s", w.Body.String())
	}
	if after, _ := st.SavedProjections(hh); len(after) != 0 {
		t.Errorf("rows survived the delete: %+v", after)
	}
}

// between returns the text between the first pre and the following post.
func between(t *testing.T, s, pre, post string) string {
	t.Helper()
	i := strings.Index(s, pre)
	if i < 0 {
		t.Fatalf("%q not found in page", pre)
	}
	rest := s[i+len(pre):]
	j := strings.Index(rest, post)
	if j < 0 {
		t.Fatalf("%q not found after %q", post, pre)
	}
	return rest[:j]
}
