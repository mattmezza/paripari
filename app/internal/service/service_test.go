package service

import (
	"testing"

	"github.com/mattmezza/paripari/internal/model"
)

// fakeRates is a deterministic RateProvider: factors are keyed "FROM>TO".
type fakeRates map[string]float64

func (f fakeRates) Convert(cents int64, from, to string) int64 {
	if from == to {
		return cents
	}
	if r, ok := f[from+">"+to]; ok {
		return scale(cents, r)
	}
	if r, ok := f[to+">"+from]; ok && r != 0 {
		return scale(cents, 1/r)
	}
	return cents
}

// testRates: 1 EUR = 0.95 CHF, 1 USD = 0.90 CHF, 1 TRY = 0.03 CHF.
var testRates = fakeRates{"EUR>CHF": 0.95, "USD>CHF": 0.90, "TRY>CHF": 0.03}

func ptr[T any](v T) *T { return &v }

func income(id, userID int64, kind string, grossYearly int64, pay int, cur string, ded ...model.IncomeDeduction) model.IncomeSource {
	return model.IncomeSource{
		ID: id, UserID: userID, Kind: kind, PayStructure: pay,
		GrossYearlyCents: grossYearly, Currency: cur, Deductions: ded,
	}
}

func commonExp(id int64, amount int64, cur string, savings bool) model.Expense {
	return model.Expense{ID: id, AmountCents: amount, Currency: cur, Category: "common", IsSavings: savings}
}

func personalExp(id, userID, amount int64, cur string, savings bool) model.Expense {
	return model.Expense{ID: id, AmountCents: amount, Currency: cur, Category: "personal", UserID: &userID, IsSavings: savings}
}

// baseInputs: partners 1/2 earning 8'954 and 11'179 CHF net monthly, income
// weighted, one 3'000 rent + 1'000 common savings, one personal expense each.
func baseInputs() Inputs {
	return Inputs{
		Household: model.Household{
			ID: 1, SplitMethod: "income_weighted", IncludeVariableIncome: true, DisplayCurrency: "CHF",
		},
		PartnerA: model.User{ID: 1, Name: "Ada"},
		PartnerB: model.User{ID: 2, Name: "Bo"},
		Incomes: []model.IncomeSource{
			income(10, 1, "fixed", 895400*12, 12, "CHF"),
			income(11, 2, "fixed", 1117900*12, 12, "CHF"),
		},
		Expenses: []model.Expense{
			commonExp(20, 300000, "CHF", false), // rent
			commonExp(21, 100000, "CHF", true),  // common savings
			personalExp(22, 1, 50000, "CHF", false),
			personalExp(23, 2, 60000, "CHF", false),
		},
		Accounts: []model.Account{
			{ID: 30, Name: "Common checking", Currency: "CHF", BalanceCents: 500000, Purpose: "checking"},
			{ID: 31, Name: "Common savings", Currency: "CHF", BalanceCents: 2000000, Purpose: "savings", IBAN: "CH99"},
		},
		Goals:            []model.Goal{{ID: 40, Name: "Safety net", TargetAmountCents: 5000000, Currency: "CHF"}},
		Rates:            testRates,
		Display:          "CHF",
		AnnualReturnRate: 0.04,
	}
}

func TestMoney(t *testing.T) {
	tests := []struct {
		cents int64
		cur   string
		want  string
	}{
		{0, "CHF", "CHF 0.00"},
		{5, "CHF", "CHF 0.05"},
		{123450, "CHF", "CHF 1'234.50"},
		{123450, "EUR", "EUR 1,234.50"},
		{-123450, "CHF", "-CHF 1'234.50"},
		{100000000, "CHF", "CHF 1'000'000.00"},
		{99, "USD", "USD 0.99"},
		{-1, "EUR", "-EUR 0.01"},
	}
	for _, tc := range tests {
		if got := Money(tc.cents, tc.cur); got != tc.want {
			t.Errorf("Money(%d,%s) = %q, want %q", tc.cents, tc.cur, got, tc.want)
		}
	}
}

func TestDivRoundAndScale(t *testing.T) {
	tests := []struct{ a, b, want int64 }{
		{10, 4, 3}, {-10, 4, -3}, {10, -4, -3}, {7, 2, 4}, {-7, 2, -4},
		{100, 12, 8}, {0, 12, 0}, {5, 0, 0},
	}
	for _, tc := range tests {
		if got := divRound(tc.a, tc.b); got != tc.want {
			t.Errorf("divRound(%d,%d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	if got := scale(101, 0.5); got != 51 { // half away from zero
		t.Errorf("scale(101,0.5) = %d, want 51", got)
	}
}
