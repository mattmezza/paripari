package store

import "github.com/mattmezza/paripari/internal/model"

// PutRate upserts one base→quote rate.
func (s *Store) PutRate(base, quote string, rate float64, fetchedAt string) error {
	if fetchedAt == "" {
		fetchedAt = now()
	}
	_, err := s.DB.Exec(`INSERT INTO fx_rates (base, quote, rate, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(base, quote) DO UPDATE SET rate = excluded.rate, fetched_at = excluded.fetched_at`,
		base, quote, rate, fetchedAt)
	return err
}

func (s *Store) Rate(base, quote string) (*model.FXRate, error) {
	var r model.FXRate
	err := s.DB.QueryRow(`SELECT base, quote, rate, fetched_at FROM fx_rates WHERE base = ? AND quote = ?`, base, quote).
		Scan(&r.Base, &r.Quote, &r.Rate, &r.FetchedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &r, nil
}

func (s *Store) Rates() ([]model.FXRate, error) {
	rows, err := s.DB.Query(`SELECT base, quote, rate, fetched_at FROM fx_rates ORDER BY base, quote`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FXRate
	for rows.Next() {
		var r model.FXRate
		if err := rows.Scan(&r.Base, &r.Quote, &r.Rate, &r.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- gold prices ---

func (s *Store) PutGoldPrice(pricePerGramCents int64, currency, fetchedAt string) error {
	if fetchedAt == "" {
		fetchedAt = now()
	}
	_, err := s.DB.Exec(`INSERT INTO gold_prices (price_per_gram_cents, currency, fetched_at) VALUES (?, ?, ?)`,
		pricePerGramCents, currency, fetchedAt)
	return err
}

// LatestGoldPrice returns the most recently fetched gold price, or ErrNotFound.
func (s *Store) LatestGoldPrice() (*model.GoldPrice, error) {
	var g model.GoldPrice
	err := s.DB.QueryRow(`SELECT id, price_per_gram_cents, currency, fetched_at FROM gold_prices
		ORDER BY fetched_at DESC, id DESC LIMIT 1`).
		Scan(&g.ID, &g.PricePerGramCents, &g.Currency, &g.FetchedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &g, nil
}
