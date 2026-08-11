// Package gold fetches and caches the gold spot price and implements
// handler.GoldProvider.
package gold

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/mattmezza/paripari/internal/store"
)

const (
	staleAfter        = 24 * time.Hour
	gramsPerTroyOunce = 31.1034768
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// RateConverter matches view.RateProvider's shape without importing view
// (avoids a needless cross-package dependency for one method).
type RateConverter interface {
	Convert(amountCents int64, from, to string) int64
}

// Provider implements handler.GoldProvider. Precedence: a household's manual
// override (if set) wins over the fetched cache; the cache is the fallback;
// no price at all is an error.
//
// ponytail: handler.GoldProvider.PricePerGramCents takes no household ID, so
// on a multi-household instance the manual override is read from the first
// household only. Self-hosted PariPari is one household per instance in
// practice; revisit if that changes.
type Provider struct {
	db    *store.Store
	rates RateConverter
}

func NewProvider(db *store.Store, rates RateConverter) *Provider {
	return &Provider{db: db, rates: rates}
}

func (p *Provider) PricePerGramCents(currency string) (int64, error) {
	if cents, from, ok := p.manualOverride(); ok {
		return p.rates.Convert(cents, from, currency), nil
	}
	gp, err := p.db.LatestGoldPrice()
	if err != nil {
		return 0, fmt.Errorf("gold: no price available: %w", err)
	}
	return p.rates.Convert(gp.PricePerGramCents, gp.Currency, currency), nil
}

func (p *Provider) manualOverride() (cents int64, currency string, ok bool) {
	ids, err := p.db.HouseholdIDs()
	if err != nil || len(ids) == 0 {
		return 0, "", false
	}
	h, err := p.db.Household(ids[0])
	if err != nil || h.ManualGoldPriceCents == nil {
		return 0, "", false
	}
	return *h.ManualGoldPriceCents, h.DisplayCurrency, true
}

// Refresher is the DailyJob: refreshes the gold_prices cache when stale.
type Refresher struct {
	db *store.Store
}

func NewRefresher(db *store.Store) *Refresher { return &Refresher{db: db} }

func (r *Refresher) Name() string { return "gold-refresh" }

func (r *Refresher) Run(ctx context.Context) error {
	fresh, err := r.isFresh()
	if err == nil && fresh {
		return nil
	}
	return r.fetch(ctx)
}

// ForceRefresh always fetches, for the settings page's manual button.
func (r *Refresher) ForceRefresh(ctx context.Context) error { return r.fetch(ctx) }

func (r *Refresher) isFresh() (bool, error) {
	gp, err := r.db.LatestGoldPrice()
	if err != nil {
		return false, err
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", gp.FetchedAt)
	if err != nil {
		return false, err
	}
	return time.Since(t) < staleAfter, nil
}

type goldAPIResp struct {
	Price float64 `json:"price"` // USD per troy ounce
}

func (r *Refresher) fetch(ctx context.Context) error {
	return r.fetchFrom(ctx, "https://api.gold-api.com/price/XAU")
}

// fetchFrom does the actual HTTP round trip; split out so tests can point it
// at an httptest server.
func (r *Refresher) fetchFrom(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "paripari")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gold: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gold: fetch: status %d", resp.StatusCode)
	}
	var gr goldAPIResp
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return fmt.Errorf("gold: decode: %w", err)
	}
	if gr.Price <= 0 {
		return fmt.Errorf("gold: invalid price %v", gr.Price)
	}
	perGramCents := int64(math.Round(gr.Price / gramsPerTroyOunce * 100))
	return r.db.PutGoldPrice(perGramCents, "USD", "")
}
