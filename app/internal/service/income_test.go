package service

import (
	"testing"

	"github.com/mattmezza/paripari/internal/model"
)

func ded(name string, amount int64, period string) model.IncomeDeduction {
	return model.IncomeDeduction{Name: name, AmountCents: amount, Period: period}
}

func dedPct(name string, bp int64) model.IncomeDeduction {
	return model.IncomeDeduction{Name: name, Period: "percent", PercentBP: bp}
}

func TestNetMonthlyPercentDeductions(t *testing.T) {
	tests := []struct {
		name          string
		src           model.IncomeSource
		wantDedYearly int64
	}{
		{
			name:          "5.30% of gross",
			src:           income(1, 1, "fixed", 12000000, 12, "CHF", dedPct("AHV", 530)),
			wantDedYearly: 636000,
		},
		{
			name:          "percent tracks gross, not the fixed amount field",
			src:           income(1, 1, "fixed", 24000000, 12, "CHF", dedPct("AHV", 530)),
			wantDedYearly: 1272000,
		},
		{
			name:          "percent and fixed stack",
			src:           income(1, 1, "fixed", 12000000, 12, "CHF", dedPct("AHV", 530), ded("tax", 100000, "monthly")),
			wantDedYearly: 636000 + 1200000,
		},
		{
			name:          "rounds to the nearest cent",
			src:           income(1, 1, "fixed", 10000033, 12, "CHF", dedPct("odd", 333)),
			wantDedYearly: 333001,
		},
		{
			name:          "100% leaves nothing",
			src:           income(1, 1, "fixed", 12000000, 12, "CHF", dedPct("all", 10000)),
			wantDedYearly: 12000000,
		},
		{
			name:          "over 100% floors net at zero, not negative",
			src:           income(1, 1, "fixed", 12000000, 12, "CHF", dedPct("absurd", 12000)),
			wantDedYearly: 14400000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NetMonthly(tt.src)
			if c.DeductionsYearlyCents != tt.wantDedYearly {
				t.Errorf("deductions = %d, want %d", c.DeductionsYearlyCents, tt.wantDedYearly)
			}
			want := tt.src.GrossYearlyCents - tt.wantDedYearly
			if want < 0 {
				want = 0
			}
			if c.NetYearlyCents != want {
				t.Errorf("net yearly = %d, want %d", c.NetYearlyCents, want)
			}
			if c.NetMonthlyCents < 0 {
				t.Errorf("net monthly went negative: %d", c.NetMonthlyCents)
			}
		})
	}
}

func TestNetMonthly(t *testing.T) {
	tests := []struct {
		name                                            string
		src                                             model.IncomeSource
		wantDedYearly, wantNetYearly, wantNet, wantInst int64
	}{
		{
			name:          "12 installments no deductions",
			src:           income(1, 1, "fixed", 12000000, 12, "CHF"),
			wantDedYearly: 0, wantNetYearly: 12000000, wantNet: 1000000, wantInst: 1000000,
		},
		{
			name:          "13th salary: planning monthly spreads it, installment is smaller",
			src:           income(1, 1, "fixed", 13000000, 13, "CHF"),
			wantDedYearly: 0, wantNetYearly: 13000000, wantNet: 1083333, wantInst: 1000000,
		},
		{
			name:          "monthly deduction normalised to yearly",
			src:           income(1, 1, "fixed", 12000000, 12, "CHF", ded("tax", 100000, "monthly")),
			wantDedYearly: 1200000, wantNetYearly: 10800000, wantNet: 900000, wantInst: 900000,
		},
		{
			name:          "yearly deduction taken as-is",
			src:           income(1, 1, "fixed", 12000000, 12, "CHF", ded("tax", 1200000, "yearly")),
			wantDedYearly: 1200000, wantNetYearly: 10800000, wantNet: 900000, wantInst: 900000,
		},
		{
			name: "mixed deductions",
			src: income(1, 1, "fixed", 12000000, 13, "CHF",
				ded("ahv", 50000, "monthly"), ded("pension", 600000, "yearly")),
			wantDedYearly: 1200000, wantNetYearly: 10800000, wantNet: 900000, wantInst: 830769,
		},
		{
			name:          "deductions exceeding gross floor at zero",
			src:           income(1, 1, "fixed", 1000000, 12, "CHF", ded("tax", 2000000, "yearly")),
			wantDedYearly: 2000000, wantNetYearly: 0, wantNet: 0, wantInst: 0,
		},
		{
			name:          "bogus pay structure falls back to 12",
			src:           income(1, 1, "variable", 1200000, 0, "CHF"),
			wantDedYearly: 0, wantNetYearly: 1200000, wantNet: 100000, wantInst: 100000,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NetMonthly(tc.src)
			if c.DeductionsYearlyCents != tc.wantDedYearly {
				t.Errorf("deductions = %d, want %d", c.DeductionsYearlyCents, tc.wantDedYearly)
			}
			if c.NetYearlyCents != tc.wantNetYearly {
				t.Errorf("net yearly = %d, want %d", c.NetYearlyCents, tc.wantNetYearly)
			}
			if c.NetMonthlyCents != tc.wantNet {
				t.Errorf("net monthly = %d, want %d", c.NetMonthlyCents, tc.wantNet)
			}
			if c.PerInstallmentCents != tc.wantInst {
				t.Errorf("per installment = %d, want %d", c.PerInstallmentCents, tc.wantInst)
			}
		})
	}
}

func TestNetMonthly13thSpreadEqualsYearly(t *testing.T) {
	c := NetMonthly(income(1, 1, "fixed", 13000000, 13, "CHF"))
	// 13 installments must add back up to the yearly net...
	if got := c.PerInstallmentCents * 13; got != c.NetYearlyCents {
		t.Errorf("13 x installment = %d, want %d", got, c.NetYearlyCents)
	}
	// ...while the planning figure spreads that 13th over 12 months, so it is
	// strictly higher than a single payday.
	if c.NetMonthlyCents <= c.PerInstallmentCents {
		t.Errorf("planning monthly %d should exceed installment %d", c.NetMonthlyCents, c.PerInstallmentCents)
	}
	if d := abs(c.NetMonthlyCents*12 - c.NetYearlyCents); d > 12 {
		t.Errorf("12 x planning monthly is off by %d cents", d)
	}
}

func TestPartnerIncomes(t *testing.T) {
	sources := []model.IncomeSource{
		income(1, 1, "fixed", 12000000, 12, "CHF"),   // 10'000/mo CHF
		income(2, 1, "variable", 1200000, 12, "EUR"), // 1'000 EUR -> 950 CHF
		income(3, 2, "fixed", 24000000, 12, "USD"),   // 20'000 USD -> 18'000 CHF
	}
	got := PartnerIncomes(sources, testRates, "CHF")

	a := got[1]
	if a.FixedMonthlyCents != 1000000 || a.VariableMonthlyCents != 95000 || a.TotalMonthlyCents != 1095000 {
		t.Errorf("partner A = %+v", a)
	}
	if len(a.Sources) != 2 {
		t.Errorf("partner A sources = %d, want 2", len(a.Sources))
	}
	b := got[2]
	if b.FixedMonthlyCents != 1800000 || b.VariableMonthlyCents != 0 || b.TotalMonthlyCents != 1800000 {
		t.Errorf("partner B = %+v", b)
	}
	if b.Currency != "CHF" {
		t.Errorf("currency = %q, want CHF", b.Currency)
	}
}

func TestPartnerIncomesNilRatesIsIdentity(t *testing.T) {
	got := PartnerIncomes([]model.IncomeSource{income(1, 1, "fixed", 1200000, 12, "TRY")}, nil, "CHF")
	if got[1].TotalMonthlyCents != 100000 {
		t.Errorf("nil rates should pass amounts through, got %d", got[1].TotalMonthlyCents)
	}
}
