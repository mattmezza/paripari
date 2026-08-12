package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
)

var payloadRE = regexp.MustCompile(`(?s)id="breakdown-payload">(.*?)</script>`)

// The analysis page renders its charts entirely from the embedded JSON, so the
// test asserts on that payload rather than on markup.
func TestExpensesAnalysisPayload(t *testing.T) {
	h, _ := newServerStore(t)
	c := sessionCookie(t, post(t, h, "/signup", url.Values{
		"name": {"Elena"}, "email": {"e@example.com"}, "password": {"paripari123"},
	}, nil))

	if w := postHX(t, h, "/income", url.Values{
		"user_id": {"1"}, "name": {"Salary"}, "kind": {"fixed"}, "pay_structure": {"12"},
		"currency": {"CHF"}, "gross_yearly": {"120000"},
	}, c); w.Code != 200 {
		t.Fatalf("POST /income: %d", w.Code)
	}
	for _, e := range []url.Values{
		{"name": {"Rent"}, "amount": {"2000"}, "currency": {"CHF"}, "category": {"common"}, "subcategory": {"housing"}},
		{"name": {"Fund"}, "amount": {"500"}, "currency": {"CHF"}, "category": {"common"}, "subcategory": {"savings"}},
		{"name": {"Gym"}, "amount": {"100"}, "currency": {"CHF"}, "category": {"personal"}, "user_id": {"1"}, "subcategory": {"health"}},
	} {
		if w := postHX(t, h, "/expenses", e, c); w.Code != 200 {
			t.Fatalf("POST /expenses %v: %d %s", e, w.Code, w.Body.String())
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/expenses/analysis", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /expenses/analysis: %d %s", w.Code, w.Body.String())
	}

	m := payloadRE.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("no breakdown payload in the page")
	}
	var p struct {
		Currency string `json:"currency"`
		Common   []struct {
			Label   string `json:"label"`
			Value   int64  `json:"value"`
			Savings bool   `json:"savings"`
		} `json:"common"`
		Personal []struct {
			Label string `json:"label"`
			A     int64  `json:"a"`
		} `json:"personal"`
		Sankey []struct {
			From, To, Kind string
			Flow           int64
		} `json:"sankey"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.Unmarshal([]byte(m[1]), &p); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	if p.Currency != "CHF" || len(p.Common) != 2 || len(p.Personal) != 1 {
		t.Fatalf("payload = %+v", p)
	}
	if p.Common[0].Label != "housing" || p.Common[0].Value != 200000 {
		t.Errorf("housing row = %+v", p.Common[0])
	}
	if !p.Common[1].Savings {
		t.Errorf("savings row not flagged: %+v", p.Common[1])
	}
	if p.Personal[0].Label != "health" || p.Personal[0].A != 10000 {
		t.Errorf("health row = %+v", p.Personal[0])
	}
	// Solo household: every flow starts at the one earner and ends at a
	// labelled node.
	var total int64
	for _, f := range p.Sankey {
		if f.From == "in:a" {
			total += f.Flow
		}
		if p.Labels[f.To] == "" {
			t.Errorf("unlabelled sankey target: %+v", f)
		}
	}
	if total != 1000000 {
		t.Errorf("outflow from the earner = %d cents, want the full 10'000 net income", total)
	}
}
