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

// The two bases disagree exactly when the partners are taxed differently:
// the same gross gap produces a narrower split on net if the higher earner
// also loses more to deductions. Figures are the real-world case that
// prompted the option.
func TestSplitRatioGrossVsNet(t *testing.T) {
	src := func(user int64, grossYearly int64, deductionBP int64) model.IncomeSource {
		return model.IncomeSource{
			UserID: user, Kind: "fixed", Currency: "CHF", GrossYearlyCents: grossYearly,
			Deductions: []model.IncomeDeduction{{Period: "percent", PercentBP: deductionBP}},
		}
	}
	// A: 149_860.00 gross, 22.7% out. B: 187_000.00 gross, 29.4% out.
	incomes := PartnerIncomes([]model.IncomeSource{
		src(1, 14986000, 2270),
		src(2, 18700000, 2940),
	}, nil, "CHF")

	hh := model.Household{SplitMethod: "income_weighted", IncludeVariableIncome: true}

	net := SplitRatio(hh, incomes, 1, 2)
	if net.Basis != "net" {
		t.Errorf("default basis = %q, want net", net.Basis)
	}
	hh.WeightBasis = "gross"
	gross := SplitRatio(hh, incomes, 1, 2)
	if gross.Basis != "gross" {
		t.Errorf("basis = %q, want gross", gross.Basis)
	}

	// Gross ignores B's heavier deductions, so B carries more of the bill.
	if gross.B <= net.B {
		t.Errorf("gross B share %.4f should exceed net B share %.4f", gross.B, net.B)
	}
	if d := gross.A + gross.B; d != 1 {
		t.Errorf("shares sum to %v, want 1", d)
	}
	// Hand-computed: 149860/336860 = 0.444905, 187000/336860 = 0.555095.
	if gross.A < 0.4448 || gross.A > 0.4450 {
		t.Errorf("gross A = %.6f, want ~0.444905", gross.A)
	}
	// And cent-exact splitting still holds on the gross ratio.
	a, b := ShareOf(8500, gross)
	if a+b != 8500 {
		t.Errorf("shares %d + %d != 8500", a, b)
	}
}

// A gross basis with variable income excluded must use the fixed gross bucket,
// not the whole gross.
func TestSplitRatioGrossExcludesVariable(t *testing.T) {
	incomes := PartnerIncomes([]model.IncomeSource{
		{UserID: 1, Kind: "fixed", Currency: "CHF", GrossYearlyCents: 12000000},
		{UserID: 1, Kind: "variable", Currency: "CHF", GrossYearlyCents: 12000000},
		{UserID: 2, Kind: "fixed", Currency: "CHF", GrossYearlyCents: 12000000},
	}, nil, "CHF")
	hh := model.Household{SplitMethod: "income_weighted", WeightBasis: "gross"}

	if r := SplitRatio(hh, incomes, 1, 2); r.A != 0.5 {
		t.Errorf("variable excluded: A = %v, want 0.5", r.A)
	}
	hh.IncludeVariableIncome = true
	if r := SplitRatio(hh, incomes, 1, 2); r.A < 0.6666 || r.A > 0.6667 {
		t.Errorf("variable included: A = %v, want ~0.6667", r.A)
	}
}

// A household nobody has joined yet must charge its one member the whole
// common pool — a fifty-fifty split against a partner who does not exist
// leaves half of every common expense owed by nobody.
func TestSplitRatio_NoPartnerYet(t *testing.T) {
	for _, method := range []string{"fifty_fifty", "income_weighted"} {
		h := model.Household{SplitMethod: method}
		r := SplitRatio(h, map[int64]PartnerIncome{1: {TotalMonthlyCents: 500000}}, 1, 0)
		if r.A != 1 || r.B != 0 {
			t.Errorf("%s: ratio = %v/%v, want 1/0", method, r.A, r.B)
		}
		if a, b := ShareOf(120000, r); a != 120000 || b != 0 {
			t.Errorf("%s: share = %d/%d, want 120000/0", method, a, b)
		}
	}
}
