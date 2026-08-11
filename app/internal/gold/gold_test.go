package gold

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

// identityRates is a RateConverter that treats every currency as CHF.
type identityRates struct{}

func (identityRates) Convert(cents int64, _, _ string) int64 { return cents }

func TestFetch_GramConversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 2000 USD/oz -> 2000/31.1034768 = 64.29... USD/gram -> 6429 cents
		w.Write([]byte(`{"name":"Gold","price":2000,"symbol":"XAU"}`))
	}))
	defer srv.Close()

	st := testStore(t)
	r := &Refresher{db: st}
	orig := httpClient
	httpClient = srv.Client()
	defer func() { httpClient = orig }()

	if err := r.fetchFrom(context.Background(), srv.URL); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	gp, err := st.LatestGoldPrice()
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if gp.Currency != "USD" {
		t.Fatalf("currency = %s, want USD", gp.Currency)
	}
	want := int64(6430) // round(2000/31.1034768*100)
	if gp.PricePerGramCents != want {
		t.Fatalf("price per gram = %d, want %d", gp.PricePerGramCents, want)
	}
}

func TestPricePerGramCents_CacheFallback(t *testing.T) {
	st := testStore(t)
	if err := st.PutGoldPrice(6429, "USD", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateHousehold("Test"); err != nil {
		t.Fatal(err)
	}

	p := NewProvider(st, identityRates{})
	got, err := p.PricePerGramCents("USD")
	if err != nil {
		t.Fatalf("price: %v", err)
	}
	if got != 6429 {
		t.Fatalf("got %d, want 6429 (cache, no manual override set)", got)
	}
}

func TestPricePerGramCents_ManualOverrideWins(t *testing.T) {
	st := testStore(t)
	if err := st.PutGoldPrice(6429, "USD", ""); err != nil {
		t.Fatal(err)
	}
	h, err := st.CreateHousehold("Test")
	if err != nil {
		t.Fatal(err)
	}
	override := int64(9999)
	h.ManualGoldPriceCents = &override
	if err := st.UpdateHouseholdSettings(h); err != nil {
		t.Fatal(err)
	}

	p := NewProvider(st, identityRates{})
	got, err := p.PricePerGramCents("USD")
	if err != nil {
		t.Fatalf("price: %v", err)
	}
	if got != 9999 {
		t.Fatalf("got %d, want manual override 9999", got)
	}
}

func TestPricePerGramCents_ErrorWhenNothingAvailable(t *testing.T) {
	st := testStore(t)
	if _, err := st.CreateHousehold("Test"); err != nil {
		t.Fatal(err)
	}
	p := NewProvider(st, identityRates{})
	if _, err := p.PricePerGramCents("USD"); err == nil {
		t.Fatal("expected error when no cache and no override")
	}
}

func TestIsFresh(t *testing.T) {
	st := testStore(t)
	r := &Refresher{db: st}
	if _, err := r.isFresh(); err == nil {
		t.Fatal("expected error/false with no price cached")
	}
	if err := st.PutGoldPrice(6429, "USD", ""); err != nil {
		t.Fatal(err)
	}
	fresh, err := r.isFresh()
	if err != nil || !fresh {
		t.Fatalf("isFresh = %v, %v, want true, nil", fresh, err)
	}

	// A store whose only (and thus latest) price is stale.
	staleStore := testStore(t)
	if err := staleStore.PutGoldPrice(6500, "USD", time.Now().Add(-48*time.Hour).UTC().Format("2006-01-02T15:04:05Z")); err != nil {
		t.Fatal(err)
	}
	staleR := &Refresher{db: staleStore}
	fresh, err = staleR.isFresh()
	if err != nil || fresh {
		t.Fatalf("isFresh (stale) = %v, %v, want false, nil", fresh, err)
	}
}
