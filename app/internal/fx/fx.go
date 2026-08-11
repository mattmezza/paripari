// Package fx fetches and caches CHF-based exchange rates and implements
// view.RateProvider for the render path.
package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mattmezza/paripari/internal/store"
)

// Currencies is the supported set (base CHF + these quotes).
var Currencies = []string{"CHF", "EUR", "TRY", "USD", "GBP"}

const staleAfter = 24 * time.Hour

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Provider implements view.RateProvider from the fx_rates cache. All rates
// are stored base=CHF, so conversion always routes through CHF.
type Provider struct {
	db *store.Store

	mu sync.Once // logs the "no rate cached" fallback once
}

func NewProvider(db *store.Store) *Provider { return &Provider{db: db} }

// Convert returns amountCents expressed in `to`. Falls back to returning the
// amount unchanged (never errors the render path) if no rate has ever been
// fetched for one of the currencies.
func (p *Provider) Convert(amountCents int64, from, to string) int64 {
	if from == to {
		return amountCents
	}
	rf, ok1 := p.rateFromCHF(from)
	rt, ok2 := p.rateFromCHF(to)
	if !ok1 || !ok2 {
		p.mu.Do(func() {
			log.Printf("fx: no cached rate for %s/%s, returning amount unchanged", from, to)
		})
		return amountCents
	}
	chf := float64(amountCents) / rf
	return int64(math.Round(chf * rt))
}

func (p *Provider) rateFromCHF(currency string) (float64, bool) {
	if currency == "CHF" {
		return 1, true
	}
	r, err := p.db.Rate("CHF", currency)
	if err != nil {
		return 0, false
	}
	return r.Rate, true
}

// Refresher is the DailyJob: refreshes the fx_rates cache when stale.
type Refresher struct {
	db *store.Store
}

func NewRefresher(db *store.Store) *Refresher { return &Refresher{db: db} }

func (r *Refresher) Name() string { return "fx-refresh" }

// Run refreshes only if the cache is stale (>24h old or empty).
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
	rate, err := r.db.Rate("CHF", "EUR")
	if err != nil {
		return false, err
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", rate.FetchedAt)
	if err != nil {
		return false, err
	}
	return time.Since(t) < staleAfter, nil
}

type frankfurterResp struct {
	Rates map[string]float64 `json:"rates"`
}

func (r *Refresher) fetch(ctx context.Context) error {
	symbols := strings.Join([]string{"EUR", "TRY", "USD", "GBP"}, ",")
	return r.fetchFrom(ctx, "https://api.frankfurter.app/latest?base=CHF&symbols="+symbols)
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
		return fmt.Errorf("fx: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fx: fetch: status %d", resp.StatusCode)
	}
	var fr frankfurterResp
	if err := json.NewDecoder(resp.Body).Decode(&fr); err != nil {
		return fmt.Errorf("fx: decode: %w", err)
	}
	for quote, rate := range fr.Rates {
		if err := r.db.PutRate("CHF", quote, rate, ""); err != nil {
			return fmt.Errorf("fx: store %s: %w", quote, err)
		}
	}
	return nil
}
