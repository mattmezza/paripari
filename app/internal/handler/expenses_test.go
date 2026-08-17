package handler_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
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

// fixedRates converts TRY→CHF at a fixed 0.095 (1000 TRY = 95 CHF) and passes
// everything else through untouched, so handler tests can exercise conversion
// without a live FX source.
type fixedRates struct{}

func (fixedRates) Convert(cents int64, from, to string) int64 {
	if from == "TRY" && to == "CHF" {
		return int64(math.Round(float64(cents) * 0.095))
	}
	return cents
}

// Regression test for #3: an expense in a currency other than the household's
// display currency must be converted before the list sums subtotals and splits
// partner shares, so the list agrees with the dashboard. The inline edit input
// keeps the native amount so edits round-trip in that currency.
func TestExpensesListConvertsNonDisplayCurrency(t *testing.T) {
	h, _ := newServerStoreRates(t, fixedRates{})
	c := sessionCookie(t, post(t, h, "/signup", url.Values{
		"name": {"Elena"}, "email": {"e@example.com"}, "password": {"paripari123"},
	}, nil))

	if w := postHX(t, h, "/expenses", url.Values{
		"name": {"Holiday"}, "amount": {"1000"}, "currency": {"TRY"},
		"category": {"common"}, "subcategory": {"travel"},
	}, c); w.Code != 200 {
		t.Fatalf("POST /expenses: %d %s", w.Code, w.Body.String())
	}

	r := httptest.NewRequest(http.MethodGet, "/expenses", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /expenses: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	// 1000 TRY at the fixed 0.095 rate is 95.00 CHF. The list must show the
	// converted figure — the raw 1'000.00 TRY labelled as CHF is the bug. The
	// apostrophe thousands separator is HTML-escaped (&#39;) in the markup.
	if strings.Contains(body, "CHF 1&#39;000.00") {
		t.Errorf("expense list reports the raw TRY amount labelled as CHF: got 1'000.00 CHF")
	}
	if !strings.Contains(body, "CHF 95.00") {
		t.Errorf("expense list does not show the converted TRY amount: want CHF 95.00")
	}
	// The inline-edit input keeps the native amount so edits round-trip in TRY.
	if !strings.Contains(body, `value="1000.00"`) {
		t.Errorf("inline edit input does not keep the native TRY amount")
	}
}
