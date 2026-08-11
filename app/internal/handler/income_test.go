package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postHX posts as htmx does, which satisfies the CSRF check without a token.
func postHX(t *testing.T, h http.Handler, path string, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// A percentage deduction typed as "5" is 5%, i.e. 500 basis points. The form
// field carries the typed number for both meanings, so this is the one place
// the ×100 could silently go missing on a money path.
func TestIncomePercentDeductionStoresBasisPoints(t *testing.T) {
	h, st := newServerStore(t)
	w := post(t, h, "/signup", url.Values{
		"name": {"Elena"}, "email": {"e@example.com"}, "password": {"paripari123"},
	}, nil)
	c := sessionCookie(t, w)

	w = postHX(t, h, "/income", url.Values{
		"user_id": {"1"}, "name": {"Salary"}, "kind": {"fixed"},
		"pay_structure": {"12"}, "currency": {"CHF"}, "gross_yearly": {"120000"},
		"deduction_name[]":   {"AHV", "tax"},
		"deduction_amount[]": {"5.3", "200"},
		"deduction_period[]": {"percent", "monthly"},
	}, c)
	if w.Code != 200 {
		t.Fatalf("POST /income: %d %s", w.Code, w.Body.String())
	}

	incs, err := st.Incomes(1)
	if err != nil || len(incs) != 1 {
		t.Fatalf("incomes: %v %d", err, len(incs))
	}
	d := incs[0].Deductions
	if len(d) != 2 {
		t.Fatalf("deductions: %d", len(d))
	}
	if d[0].PercentBP != 530 {
		t.Errorf("percent deduction = %d bp, want 530", d[0].PercentBP)
	}
	if d[1].AmountCents != 20000 {
		t.Errorf("fixed deduction = %d cents, want 20000", d[1].AmountCents)
	}
}

// The dashboard must say what "net income" is made of, and warn when the
// deductions behind it are missing or were typed 100x too small.
func TestDashboardNetIncomeNote(t *testing.T) {
	cases := []struct {
		name, amount, want string
	}{
		{"normal rate is explained", "5.3", "less 5.3% of deductions"},
		{"rate typed as a fraction is flagged", "0.05", "Only 0.05% of gross is deducted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newServerStore(t)
			c := sessionCookie(t, post(t, h, "/signup", url.Values{
				"name": {"Elena"}, "email": {"e@example.com"}, "password": {"paripari123"},
			}, nil))
			w := postHX(t, h, "/income", url.Values{
				"user_id": {"1"}, "name": {"Salary"}, "kind": {"fixed"}, "pay_structure": {"12"},
				"currency": {"CHF"}, "gross_yearly": {"120000"},
				"deduction_name[]": {"AHV"}, "deduction_amount[]": {tc.amount},
				"deduction_period[]": {"percent"},
			}, c)
			if w.Code != 200 {
				t.Fatalf("POST /income: %d", w.Code)
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.AddCookie(c)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("GET /: %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Errorf("dashboard does not mention %q", tc.want)
			}
		})
	}
}
