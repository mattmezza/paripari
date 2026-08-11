package service

import "github.com/mattmezza/paripari/internal/model"

// IncomeCalc is one income source, fully resolved. Amounts are in the source's
// native Currency.
type IncomeCalc struct {
	Source                model.IncomeSource
	DeductionsYearlyCents int64
	NetYearlyCents        int64
	// NetMonthlyCents is the PLANNING figure: net yearly / 12, regardless of
	// pay structure. A 13th salary is real money that arrives once a year, but
	// budgeting against 13 months of cash in a 12-month year is how people end
	// up short; PariPari spreads it. PerInstallmentCents is what actually lands
	// in the bank on payday (net yearly / 12 or / 13).
	NetMonthlyCents     int64
	PerInstallmentCents int64
}

// NetMonthly resolves one income source. Deductions are normalised to yearly
// (monthly ones × 12, percentages applied to gross yearly) and are assumed to be
// in the source's currency, as the schema gives them no currency of their own.
// Net is floored at zero.
func NetMonthly(src model.IncomeSource) IncomeCalc {
	c := IncomeCalc{Source: src}
	for _, d := range src.Deductions {
		switch d.Period {
		case "percent":
			c.DeductionsYearlyCents += DeductionYearly(d, src.GrossYearlyCents)
		case "monthly":
			c.DeductionsYearlyCents += d.AmountCents * 12
		default:
			c.DeductionsYearlyCents += d.AmountCents
		}
	}
	c.NetYearlyCents = src.GrossYearlyCents - c.DeductionsYearlyCents
	if c.NetYearlyCents < 0 {
		c.NetYearlyCents = 0
	}
	c.NetMonthlyCents = divRound(c.NetYearlyCents, 12)
	installments := int64(src.PayStructure)
	if installments != 12 && installments != 13 {
		installments = 12
	}
	c.PerInstallmentCents = divRound(c.NetYearlyCents, installments)
	return c
}

// DeductionYearly is what one deduction costs per year against the given gross.
// Percentages are basis points of gross; fixed amounts ignore gross entirely.
func DeductionYearly(d model.IncomeDeduction, grossYearlyCents int64) int64 {
	switch d.Period {
	case "percent":
		return divRound(grossYearlyCents*d.PercentBP, 10000)
	case "monthly":
		return d.AmountCents * 12
	default:
		return d.AmountCents
	}
}

// PartnerIncome aggregates one partner's sources, converted into a single
// currency.
type PartnerIncome struct {
	UserID               int64
	Currency             string
	FixedMonthlyCents    int64
	VariableMonthlyCents int64
	TotalMonthlyCents    int64
	// GrossMonthlyCents is gross yearly ÷ 12 across every source;
	// DeductionsMonthlyCents is exactly Gross − Total, so the three always
	// reconcile on screen even after per-source rounding and conversion.
	GrossMonthlyCents      int64
	// GrossFixedMonthlyCents excludes variable sources, mirroring
	// FixedMonthlyCents — the gross-basis split needs the same choice.
	GrossFixedMonthlyCents int64
	DeductionsMonthlyCents int64
	Sources                []IncomeCalc
}

// DeductionRateBP is deductions as basis points of gross (5.3% -> 530), the
// unit the pct100 template func renders. Zero gross means zero rate.
func (p PartnerIncome) DeductionRateBP() int64 {
	return rateBP(p.DeductionsMonthlyCents, p.GrossMonthlyCents)
}

func rateBP(part, whole int64) int64 {
	if whole <= 0 {
		return 0
	}
	return divRound(part*10000, whole)
}

// PartnerIncomes groups income sources per user, converting each source's net
// monthly into `display` via rates. A nil rates provider means 1:1.
func PartnerIncomes(sources []model.IncomeSource, rates Rates, display string) map[int64]PartnerIncome {
	out := map[int64]PartnerIncome{}
	for _, s := range sources {
		c := NetMonthly(s)
		p := out[s.UserID]
		p.UserID, p.Currency = s.UserID, display
		p.Sources = append(p.Sources, c)
		net := conv(rates, c.NetMonthlyCents, s.Currency, display)
		if s.Kind == "variable" {
			p.VariableMonthlyCents += net
		} else {
			p.FixedMonthlyCents += net
		}
		p.TotalMonthlyCents = p.FixedMonthlyCents + p.VariableMonthlyCents
		gross := conv(rates, divRound(s.GrossYearlyCents, 12), s.Currency, display)
		p.GrossMonthlyCents += gross
		if s.Kind != "variable" {
			p.GrossFixedMonthlyCents += gross
		}
		p.DeductionsMonthlyCents += gross - net
		out[s.UserID] = p
	}
	return out
}
