package service

import (
	"math"
	"testing"

	"github.com/mattmezza/paripari/internal/model"
)

func hh(method string, includeVariable bool) model.Household {
	return model.Household{ID: 1, SplitMethod: method, IncludeVariableIncome: includeVariable}
}

func TestSplitRatio(t *testing.T) {
	// prompt.md's worked example: CHF 8'954 vs CHF 11'179 net monthly.
	// The exact ratio is 44.474% / 55.526% — prompt.md quotes 44.49/55.51,
	// which is a rounding slip in the prose, not a different formula.
	worked := map[int64]PartnerIncome{
		1: {UserID: 1, FixedMonthlyCents: 895400, TotalMonthlyCents: 895400},
		2: {UserID: 2, FixedMonthlyCents: 1117900, TotalMonthlyCents: 1117900},
	}
	tests := []struct {
		name    string
		h       model.Household
		incomes map[int64]PartnerIncome
		wantA   float64
	}{
		{"fifty fifty ignores income", hh("fifty_fifty", true), worked, 0.5},
		{"income weighted worked example", hh("income_weighted", true), worked, 895400.0 / 2013300.0},
		{
			"variable income included",
			hh("income_weighted", true),
			map[int64]PartnerIncome{
				1: {FixedMonthlyCents: 500000, VariableMonthlyCents: 500000, TotalMonthlyCents: 1000000},
				2: {FixedMonthlyCents: 1000000, TotalMonthlyCents: 1000000},
			},
			0.5,
		},
		{
			"variable income excluded",
			hh("income_weighted", false),
			map[int64]PartnerIncome{
				1: {FixedMonthlyCents: 500000, VariableMonthlyCents: 500000, TotalMonthlyCents: 1000000},
				2: {FixedMonthlyCents: 1000000, TotalMonthlyCents: 1000000},
			},
			1.0 / 3.0,
		},
		{"zero income falls back to 50/50", hh("income_weighted", true), map[int64]PartnerIncome{}, 0.5},
		{
			"single earner takes it all",
			hh("income_weighted", true),
			map[int64]PartnerIncome{1: {TotalMonthlyCents: 1000000}},
			1.0,
		},
		{"unknown method treated as fifty fifty", hh("", true), worked, 0.5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := SplitRatio(tc.h, tc.incomes, 1, 2)
			if math.Abs(r.A-tc.wantA) > 1e-9 {
				t.Errorf("A = %v, want %v", r.A, tc.wantA)
			}
			if math.Abs(r.A+r.B-1) > 1e-12 {
				t.Errorf("A+B = %v, want 1", r.A+r.B)
			}
			if r.AUserID != 1 || r.BUserID != 2 {
				t.Errorf("user ids = %d/%d", r.AUserID, r.BUserID)
			}
		})
	}
}

func TestSplitRatioWorkedExamplePercent(t *testing.T) {
	r := SplitRatio(hh("income_weighted", true), map[int64]PartnerIncome{
		1: {TotalMonthlyCents: 895400},
		2: {TotalMonthlyCents: 1117900},
	}, 1, 2)
	if got := math.Round(r.A*10000) / 100; got != 44.47 {
		t.Errorf("A%% = %v, want 44.47", got)
	}
	if got := math.Round(r.B*10000) / 100; got != 55.53 {
		t.Errorf("B%% = %v, want 55.53", got)
	}
}

func TestShareOfSumsToTotal(t *testing.T) {
	tests := []struct {
		amount int64
		ratio  float64
		wantA  int64
	}{
		{100000, 0.5, 50000},
		{1, 0.5, 1}, // half a cent rounds away from zero
		{3, 0.5, 2},
		{100001, 0.4447, 44470},
		{0, 0.4447, 0},
		{-50000, 0.5, -25000}, // refunds split too
	}
	for _, tc := range tests {
		a, b := ShareOf(tc.amount, Ratio{A: tc.ratio, B: 1 - tc.ratio})
		if a != tc.wantA {
			t.Errorf("ShareOf(%d,%v) A = %d, want %d", tc.amount, tc.ratio, a, tc.wantA)
		}
		if a+b != tc.amount {
			t.Errorf("ShareOf(%d,%v) = %d+%d, must sum to total", tc.amount, tc.ratio, a, b)
		}
	}
}

// Property: for any odd-ish amount and any ratio, the two shares always sum
// back to the total, exactly.
func TestShareOfRoundingInvariant(t *testing.T) {
	ratios := []float64{0, 0.0001, 1.0 / 3.0, 0.4447, 0.5, 0.55526, 0.9999, 1}
	for _, r := range ratios {
		ratio := Ratio{A: r, B: 1 - r}
		for amount := int64(1); amount < 20000; amount += 7 {
			a, b := ShareOf(amount, ratio)
			if a+b != amount {
				t.Fatalf("ratio %v amount %d: %d+%d != %d", r, amount, a, b, amount)
			}
			if a < 0 || b < 0 {
				t.Fatalf("ratio %v amount %d: negative share %d/%d", r, amount, a, b)
			}
		}
	}
}
