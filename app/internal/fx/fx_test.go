package fx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattmezza/paripari/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func TestConvert_Identity(t *testing.T) {
	p := NewProvider(testStore(t))
	if got := p.Convert(1000, "CHF", "CHF"); got != 1000 {
		t.Fatalf("identity convert: got %d", got)
	}
}

func TestConvert_CrossRate(t *testing.T) {
	st := testStore(t)
	// 1 CHF = 1.10 EUR, 1 CHF = 1.25 USD.
	if err := st.PutRate("CHF", "EUR", 1.10, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.PutRate("CHF", "USD", 1.25, ""); err != nil {
		t.Fatal(err)
	}
	p := NewProvider(st)

	// CHF -> EUR: 100.00 CHF * 1.10 = 110.00 EUR
	if got := p.Convert(10000, "CHF", "EUR"); got != 11000 {
		t.Fatalf("CHF->EUR: got %d, want 11000", got)
	}
	// EUR -> CHF: 110.00 EUR / 1.10 = 100.00 CHF
	if got := p.Convert(11000, "EUR", "CHF"); got != 10000 {
		t.Fatalf("EUR->CHF: got %d, want 10000", got)
	}
	// cross rate EUR -> USD, routed through CHF:
	// 110 EUR -> 100 CHF -> 125 USD
	if got := p.Convert(11000, "EUR", "USD"); got != 12500 {
		t.Fatalf("EUR->USD: got %d, want 12500", got)
	}
}

func TestConvert_OfflineFallback(t *testing.T) {
	// No rate ever fetched: never error the render path, return unchanged.
	p := NewProvider(testStore(t))
	if got := p.Convert(500, "USD", "EUR"); got != 500 {
		t.Fatalf("fallback: got %d, want 500 unchanged", got)
	}
}

func TestRefresher_FetchAndCacheFreshness(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"amount":1,"base":"CHF","date":"2024-01-01","rates":{"EUR":1.05,"USD":1.10,"GBP":0.88,"TRY":34.5}}`))
	}))
	defer srv.Close()

	st := testStore(t)
	r := &Refresher{db: st}
	orig := httpClient
	httpClient = srv.Client()
	defer func() { httpClient = orig }()

	// monkeypatch the URL by hitting fetch directly via a small wrapper: since
	// fetch hardcodes the frankfurter URL, test isFresh + PutRate/Rate wiring
	// with a direct fetchFrom call instead.
	if err := r.fetchFrom(context.Background(), srv.URL); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	rate, err := st.Rate("CHF", "EUR")
	if err != nil {
		t.Fatalf("rate lookup: %v", err)
	}
	if rate.Rate != 1.05 {
		t.Fatalf("rate = %v, want 1.05", rate.Rate)
	}

	fresh, err := r.isFresh()
	if err != nil || !fresh {
		t.Fatalf("isFresh = %v, %v, want true, nil", fresh, err)
	}

	// Run() should not re-fetch when fresh.
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Run() re-fetched while fresh: calls = %d", calls)
	}

	// Simulate staleness: backdate the cached rate.
	if err := st.PutRate("CHF", "EUR", 1.05, time.Now().Add(-48*time.Hour).UTC().Format("2006-01-02T15:04:05Z")); err != nil {
		t.Fatal(err)
	}
	fresh, err = r.isFresh()
	if err != nil || fresh {
		t.Fatalf("isFresh after backdate = %v, %v, want false, nil", fresh, err)
	}
}
