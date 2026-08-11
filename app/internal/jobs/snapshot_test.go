package jobs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
)

type identityRates struct{}

func (identityRates) Convert(cents int64, _, _ string) int64 { return cents }

type fixedGold struct{ cents int64 }

func (f fixedGold) PricePerGramCents(string) (int64, error) { return f.cents, nil }

func TestSnapshotHousehold(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	h, err := st.CreateHousehold("Test")
	if err != nil {
		t.Fatalf("household: %v", err)
	}
	if _, err := st.CreateAccount(&model.Account{HouseholdID: h.ID, Name: "Checking", Currency: "CHF", BalanceCents: 100000, Purpose: "checking"}); err != nil {
		t.Fatalf("account: %v", err)
	}
	if _, err := st.CreateGoldItem(&model.GoldItem{HouseholdID: h.ID, Name: "Bar", WeightGrams: 10, PurityKarat: 24, Quantity: 1, Location: "safe"}); err != nil {
		t.Fatalf("gold item: %v", err)
	}
	if _, err := st.CreateAsset(&model.Asset{HouseholdID: h.ID, Name: "Flat", Kind: "real_estate", EstimatedValueCents: 50000000, Currency: "CHF"}); err != nil {
		t.Fatalf("asset: %v", err)
	}
	if _, err := st.CreateAsset(&model.Asset{HouseholdID: h.ID, Name: "Car", Kind: "other", EstimatedValueCents: 999999, Currency: "CHF"}); err != nil {
		t.Fatalf("asset: %v", err)
	}

	// gold priced at 6000 cents/gram (24K): 10g * 1.0 * 6000 = 60000 cents
	s := NewSnapshotter(st, identityRates{}, fixedGold{cents: 6000})
	if err := s.SnapshotHousehold(context.Background(), h.ID); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	sn, err := st.LatestSnapshot(h.ID)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if sn.LiquidCents != 100000 {
		t.Errorf("liquid = %d, want 100000", sn.LiquidCents)
	}
	if sn.AlternativeCents != 60000 {
		t.Errorf("alternative = %d, want 60000", sn.AlternativeCents)
	}
	if sn.RealEstateCents != 50000000 {
		t.Errorf("real estate = %d, want 50000000 (the 'other' asset must not count)", sn.RealEstateCents)
	}

	// Package-level hook, wired via Default.
	Default = s
	if err := SnapshotHousehold(context.Background(), h.ID); err != nil {
		t.Fatalf("package hook: %v", err)
	}
}
