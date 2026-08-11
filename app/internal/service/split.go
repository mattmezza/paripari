package service

import "github.com/mattmezza/paripari/internal/model"

// Ratio is the household contribution split. A + B always == 1.
type Ratio struct {
	AUserID, BUserID int64
	A, B             float64
	Method           string // fifty_fifty | income_weighted
	Basis            string // net | gross, meaningful only for income_weighted
}

// SplitRatio computes the contribution ratio for the two partners. With
// income_weighted the ratio follows monthly income — net by default, gross
// when the household sets WeightBasis to "gross" (variable income included
// only when the household says so). Zero total income falls back to 50/50, as
// does the fifty_fifty method.
func SplitRatio(h model.Household, incomes map[int64]PartnerIncome, aUserID, bUserID int64) Ratio {
	r := Ratio{AUserID: aUserID, BUserID: bUserID, A: 0.5, B: 0.5, Method: h.SplitMethod}
	if h.SplitMethod != "income_weighted" {
		r.Method = "fifty_fifty"
		return r
	}
	if r.Basis = h.WeightBasis; r.Basis != "gross" {
		r.Basis = "net"
	}
	a, b := ratioBase(incomes[aUserID], h), ratioBase(incomes[bUserID], h)
	total := a + b
	if total <= 0 {
		return r
	}
	r.A = float64(a) / float64(total)
	r.B = 1 - r.A
	return r
}

func ratioBase(p PartnerIncome, h model.Household) int64 {
	if h.WeightBasis == "gross" {
		if h.IncludeVariableIncome {
			return p.GrossMonthlyCents
		}
		return p.GrossFixedMonthlyCents
	}
	if h.IncludeVariableIncome {
		return p.TotalMonthlyCents
	}
	return p.FixedMonthlyCents
}

// ShareOf splits amountCents by the ratio, cent-exact: A is rounded, B takes
// the remainder, so a+b == amountCents always.
func ShareOf(amountCents int64, r Ratio) (a, b int64) {
	a = scale(amountCents, r.A)
	return a, amountCents - a
}
