package service

import (
	"math"
	"testing"

	"github.com/mattmezza/paripari/internal/model"
)

func TestBuildOverview(t *testing.T) {
	ov := BuildOverview(baseInputs())

	if math.Abs(ov.Ratio.A-895400.0/2013300.0) > 1e-9 {
		t.Fatalf("ratio A = %v", ov.Ratio.A)
	}
	checks := []struct {
		name string
		got  int64
		want int64
	}{
		{"A net income", ov.A.NetIncomeCents, 895400},
		{"B net income", ov.B.NetIncomeCents, 1117900},
		{"A common share", ov.A.CommonShareCents, 177897},
		{"B common share", ov.B.CommonShareCents, 222103},
		{"A common savings share", ov.A.CommonSavingsShareCents, 44474},
		{"B common savings share", ov.B.CommonSavingsShareCents, 55526},
		{"A personal expenses", ov.A.PersonalExpensesCents, 50000},
		{"A available", ov.A.AvailableCents, 667503},
		{"B available", ov.B.AvailableCents, 835797},
		{"common expenses", ov.CommonExpensesCents, 300000},
		{"common savings", ov.CommonSavingsCents, 100000},
		{"total income", ov.TotalIncomeCents, 2013300},
		{"total savings", ov.TotalSavingsCents, 100000},
		{"surplus", ov.SurplusCents, 1603300},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	// Shares of every common expense must reconstruct the total exactly.
	if ov.A.CommonShareCents+ov.B.CommonShareCents != 400000 {
		t.Errorf("common shares do not sum to 400000")
	}
	// Surplus identity: income - non-savings expenses.
	if ov.SurplusCents != ov.TotalIncomeCents-(ov.CommonExpensesCents+ov.PersonalExpensesCents) {
		t.Errorf("surplus identity broken: %d", ov.SurplusCents)
	}
}

func TestBuildOverviewNegativeAvailable(t *testing.T) {
	in := baseInputs()
	in.Household.SplitMethod = "fifty_fifty"
	in.Expenses = append(in.Expenses, commonExp(24, 2000000, "CHF", false)) // unaffordable
	ov := BuildOverview(in)

	if ov.A.AvailableCents >= 0 {
		t.Errorf("A available = %d, want negative", ov.A.AvailableCents)
	}
	if ov.AvailableCents != ov.A.AvailableCents+ov.B.AvailableCents {
		t.Errorf("household available must be the sum of partners")
	}
	if ov.SurplusCents >= 0 {
		t.Errorf("surplus = %d, want negative", ov.SurplusCents)
	}
}

func TestBuildOverviewZeroIncome(t *testing.T) {
	in := baseInputs()
	in.Incomes = nil
	ov := BuildOverview(in)
	if ov.Ratio.A != 0.5 || ov.Ratio.B != 0.5 {
		t.Errorf("zero income must fall back to 50/50, got %v/%v", ov.Ratio.A, ov.Ratio.B)
	}
	if ov.A.AvailableCents != -(200000 + 50000) {
		t.Errorf("A available = %d", ov.A.AvailableCents)
	}
}

func TestBuildOverviewMultiCurrency(t *testing.T) {
	in := baseInputs()
	in.Expenses = []model.Expense{
		commonExp(20, 100000, "EUR", false),     // 1'000 EUR -> 950 CHF
		personalExp(21, 1, 100000, "USD", true), // 1'000 USD -> 900 CHF savings
	}
	ov := BuildOverview(in)
	if ov.CommonExpensesCents != 95000 {
		t.Errorf("common expenses = %d, want 95000", ov.CommonExpensesCents)
	}
	if ov.A.PersonalSavingsCents != 90000 || ov.PersonalSavingsCents != 90000 {
		t.Errorf("personal savings = %d/%d, want 90000", ov.A.PersonalSavingsCents, ov.PersonalSavingsCents)
	}
	if ov.A.TotalSavingsCents != 90000+ov.A.CommonSavingsShareCents {
		t.Errorf("A total savings = %d", ov.A.TotalSavingsCents)
	}
}

func TestBuildOverviewOrphanPersonalExpenseIgnored(t *testing.T) {
	in := baseInputs()
	in.Expenses = []model.Expense{personalExp(20, 99, 100000, "CHF", false)} // unknown user
	ov := BuildOverview(in)
	if ov.PersonalExpensesCents != 0 {
		t.Errorf("orphan personal expense should be ignored, got %d", ov.PersonalExpensesCents)
	}
}
