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

func (f fixedGold) PricePerGramCents(int64, string) (int64, error) { return f.cents, nil }

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

func TestFinancialSnapshotUpsert(t *testing.T) {
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

	tests := []struct {
		name string
		in   model.FinancialSnapshot
		want model.FinancialSnapshot
	}{
		{"first write", model.FinancialSnapshot{
			Date: "2026-01-01", IncomeCents: 1000, ExpensesCents: 400, SavingsCents: 300,
			AvailableCents: 300, CommonExpensesCents: 250, PersonalExpensesCents: 150,
		}, model.FinancialSnapshot{
			Date: "2026-01-01", Currency: "CHF", IncomeCents: 1000, ExpensesCents: 400,
			SavingsCents: 300, AvailableCents: 300, CommonExpensesCents: 250, PersonalExpensesCents: 150,
		}},
		{"same day replaces", model.FinancialSnapshot{
			Date: "2026-01-01", IncomeCents: 2000, ExpensesCents: 900, SavingsCents: 100,
			AvailableCents: 1000, CommonExpensesCents: 600, PersonalExpensesCents: 300,
		}, model.FinancialSnapshot{
			Date: "2026-01-01", Currency: "CHF", IncomeCents: 2000, ExpensesCents: 900,
			SavingsCents: 100, AvailableCents: 1000, CommonExpensesCents: 600, PersonalExpensesCents: 300,
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sn := tc.in
			sn.HouseholdID = h.ID
			if err := st.PutFinancialSnapshot(&sn); err != nil {
				t.Fatalf("put: %v", err)
			}
			got, err := st.FinancialSnapshots(h.ID, "")
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("rows = %d, want 1 (same-day upsert must replace)", len(got))
			}
			want := tc.want
			want.ID, want.HouseholdID = got[0].ID, h.ID
			if got[0] != want {
				t.Errorf("got %+v, want %+v", got[0], want)
			}
		})
	}

	// A different date appends rather than replacing.
	if err := st.PutFinancialSnapshot(&model.FinancialSnapshot{
		HouseholdID: h.ID, Date: "2026-02-01", IncomeCents: 5,
	}); err != nil {
		t.Fatalf("put second date: %v", err)
	}
	all, err := st.FinancialSnapshots(h.ID, "")
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(all) != 2 || all[0].Date != "2026-01-01" {
		t.Fatalf("want 2 rows oldest-first, got %d: %+v", len(all), all)
	}
	// `since` filters.
	recent, err := st.FinancialSnapshots(h.ID, "2026-01-15")
	if err != nil {
		t.Fatalf("read since: %v", err)
	}
	if len(recent) != 1 || recent[0].Date != "2026-02-01" {
		t.Fatalf("since filter: got %+v", recent)
	}
}

// The daily job writes both snapshot kinds for the same household.
func TestSnapshotHouseholdWritesFinancial(t *testing.T) {
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
	u, err := st.CreateUser(h.ID, "a@example.com", "x", "A")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := st.CreateIncome(&model.IncomeSource{
		HouseholdID: h.ID, UserID: u.ID, Name: "Job", Kind: "fixed",
		GrossYearlyCents: 1200000, PayStructure: 12, Currency: "CHF",
	}); err != nil {
		t.Fatalf("income: %v", err)
	}
	if _, err := st.CreateExpense(&model.Expense{
		HouseholdID: h.ID, Name: "Rent", AmountCents: 20000, Currency: "CHF", Category: "common",
	}); err != nil {
		t.Fatalf("expense: %v", err)
	}

	s := NewSnapshotter(st, identityRates{}, fixedGold{cents: 0})
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	fin, err := st.FinancialSnapshots(h.ID, "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(fin) != 1 {
		t.Fatalf("financial snapshots = %d, want 1", len(fin))
	}
	if fin[0].IncomeCents != 100000 {
		t.Errorf("income = %d, want 100000", fin[0].IncomeCents)
	}
	if fin[0].ExpensesCents != 20000 {
		t.Errorf("expenses = %d, want 20000", fin[0].ExpensesCents)
	}
	// Re-running the same day must not duplicate.
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if fin, _ := st.FinancialSnapshots(h.ID, ""); len(fin) != 1 {
		t.Fatalf("rerun duplicated: %d rows", len(fin))
	}
}
