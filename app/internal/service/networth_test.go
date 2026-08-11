package service

import (
	"math"
	"testing"

	"github.com/mattmezza/paripari/internal/model"
)

func TestPurity(t *testing.T) {
	tests := []struct {
		karat int
		want  float64
	}{
		{24, 1.0}, {22, 0.9167}, {18, 0.75}, {14, 0.5833},
		{12, 0.5}, {0, 0}, {-1, 0},
	}
	for _, tc := range tests {
		if got := Purity(tc.karat); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Purity(%d) = %v, want %v", tc.karat, got, tc.want)
		}
	}
}

func TestGoldValue(t *testing.T) {
	const spot = 700000 // CHF 7'000.00 per gram of 24K
	tests := []struct {
		name string
		item model.GoldItem
		want int64
	}{
		{"24K single gram", model.GoldItem{WeightGrams: 1, PurityKarat: 24, Quantity: 1}, 700000},
		{"22K", model.GoldItem{WeightGrams: 1, PurityKarat: 22, Quantity: 1}, 641690},
		{"18K", model.GoldItem{WeightGrams: 1, PurityKarat: 18, Quantity: 1}, 525000},
		{"14K", model.GoldItem{WeightGrams: 1, PurityKarat: 14, Quantity: 1}, 408310},
		{"quantity multiplies", model.GoldItem{WeightGrams: 10, PurityKarat: 24, Quantity: 3}, 21000000},
		{"fractional weight", model.GoldItem{WeightGrams: 2.5, PurityKarat: 18, Quantity: 1}, 1312500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GoldValue(tc.item, spot); got != tc.want {
				t.Errorf("GoldValue = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestComputeNetWorth(t *testing.T) {
	in := baseInputs()
	in.Accounts = append(in.Accounts,
		model.Account{ID: 32, Currency: "EUR", BalanceCents: 100000, Purpose: "investment"}, // -> 95'000 CHF
		model.Account{ID: 33, Currency: "USD", BalanceCents: 100000, Purpose: "cc_buffer"},  // -> 90'000 CHF
	)
	in.Gold = []model.GoldItem{
		{WeightGrams: 10, PurityKarat: 24, Quantity: 1},
		{WeightGrams: 10, PurityKarat: 18, Quantity: 2},
	}
	in.GoldPricePerGramCents = 700000
	in.Assets = []model.Asset{
		{EstimatedValueCents: 50000000, Currency: "CHF"},
		{EstimatedValueCents: 10000000, Currency: "EUR"}, // -> 9'500'000 CHF
	}

	nw := ComputeNetWorth(in)
	if nw.LiquidCents != 500000+2000000+95000+90000 {
		t.Errorf("liquid = %d", nw.LiquidCents)
	}
	if nw.ByPurpose["investment"] != 95000 || nw.ByPurpose["savings"] != 2000000 {
		t.Errorf("by purpose = %+v", nw.ByPurpose)
	}
	if nw.AlternativeCents != 7000000+10500000 {
		t.Errorf("alternative = %d", nw.AlternativeCents)
	}
	if math.Abs(nw.GrossGrams-30) > 1e-9 || math.Abs(nw.FineGrams-25) > 1e-9 {
		t.Errorf("grams = %v gross / %v fine", nw.GrossGrams, nw.FineGrams)
	}
	if nw.RealEstateCents != 59500000 {
		t.Errorf("real estate = %d", nw.RealEstateCents)
	}
	if nw.TotalCents != nw.LiquidCents+nw.AlternativeCents+nw.RealEstateCents {
		t.Errorf("total mismatch")
	}
	if nw.LiquidOnlyCents() != nw.LiquidCents {
		t.Errorf("liquid-only view must equal the liquid bucket")
	}
}

func TestComputeNetWorthEmpty(t *testing.T) {
	nw := ComputeNetWorth(Inputs{Display: "CHF"})
	if nw.TotalCents != 0 || nw.Currency != "CHF" {
		t.Errorf("empty net worth = %+v", nw)
	}
}

func TestGoalProgresses(t *testing.T) {
	in := baseInputs()
	in.Goals = []model.Goal{
		{ID: 40, Name: "Safety net", TargetAmountCents: 5000000, Currency: "CHF"},
		{ID: 41, Name: "Done", TargetAmountCents: 100000, Currency: "CHF"},
		{ID: 42, Name: "Euro goal", TargetAmountCents: 5000000, Currency: "EUR"},
	}
	got := GoalProgresses(in, BuildOverview(in))
	if len(got) != 3 {
		t.Fatalf("goals = %d", len(got))
	}
	if got[0].CurrentCents != 2000000 || math.Abs(got[0].Ratio-0.4) > 1e-9 {
		t.Errorf("safety net = %+v", got[0])
	}
	if got[0].ETAMonths <= 0 {
		t.Errorf("safety net ETA = %d, want a positive month count", got[0].ETAMonths)
	}
	if got[1].ETAMonths != 0 || !got[1].ReachedAlready || got[1].Ratio != 1 {
		t.Errorf("reached goal = %+v", got[1])
	}
	if got[2].CurrentCents == got[0].CurrentCents {
		t.Errorf("EUR goal progress should be converted, got %d", got[2].CurrentCents)
	}
}

func TestGoalProgressUnreachableWithoutSurplus(t *testing.T) {
	in := baseInputs()
	in.Incomes = nil
	in.AnnualReturnRate = 0
	got := GoalProgresses(in, BuildOverview(in))
	if got[0].ETAMonths != -1 {
		t.Errorf("ETA = %d, want -1", got[0].ETAMonths)
	}
}
