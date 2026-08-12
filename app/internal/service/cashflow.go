package service

import "github.com/mattmezza/paripari/internal/model"

// Inputs is the single struct handlers assemble from the store and hand to the
// engine. Everything downstream (cash flow, split, transfers, projections,
// scenarios, trips, net worth) is a pure function of it.
type Inputs struct {
	Household model.Household
	// PartnerA and PartnerB are the two household members. PartnerA is the
	// lower user id by convention (store returns them ordered).
	PartnerA, PartnerB model.User
	Incomes            []model.IncomeSource
	Expenses           []model.Expense
	Accounts           []model.Account
	Assets             []model.Asset
	Gold               []model.GoldItem
	Goals              []model.Goal

	Rates   Rates
	Display string // display currency; "" means "leave everything as-is"
	// GoldPricePerGramCents is the 24K spot price per gram in Display currency.
	GoldPricePerGramCents int64
	// AnnualReturnRate is the projection assumption, e.g. 0.05 for 5%.
	AnnualReturnRate float64
}

// amount converts an expense/account/asset amount into the display currency.
func (in Inputs) amount(cents int64, from string) int64 {
	return conv(in.Rates, cents, from, in.Display)
}

// PartnerCashflow is one partner's monthly picture, all in Display currency.
type PartnerCashflow struct {
	UserID                  int64
	Name                    string
	ShareRatio              float64
	NetIncomeCents          int64
	GrossIncomeCents        int64 // gross yearly ÷ 12, before deductions
	DeductionsCents         int64 // GrossIncomeCents − NetIncomeCents
	FixedIncomeCents        int64
	VariableIncomeCents     int64
	PersonalExpensesCents   int64 // personal, non-savings
	PersonalSavingsCents    int64
	CommonShareCents        int64 // share of ALL common expenses, savings included
	CommonSavingsShareCents int64
	// TotalOutCents is everything leaving the partner each month.
	TotalOutCents int64
	// AvailableCents is net income minus everything out. May be negative.
	AvailableCents int64
	// TotalSavingsCents is personal savings + share of common savings.
	TotalSavingsCents int64
}

// DeductionRateBP is deductions as basis points of gross (5.3% -> 530), for the
// pct100 template func.
func (p PartnerCashflow) DeductionRateBP() int64 {
	return rateBP(p.DeductionsCents, p.GrossIncomeCents)
}

// MonthlyOverview is what the dashboard renders directly.
type MonthlyOverview struct {
	Currency string
	Ratio    Ratio
	A, B     PartnerCashflow

	CommonExpensesCents   int64 // non-savings common
	CommonSavingsCents    int64
	PersonalExpensesCents int64 // non-savings personal, both partners
	PersonalSavingsCents  int64

	TotalIncomeCents      int64 // net, both partners
	TotalGrossIncomeCents int64
	TotalDeductionsCents  int64
	TotalExpensesCents    int64 // everything, savings included
	TotalSavingsCents     int64
	AvailableCents        int64
	// SurplusCents is income minus non-savings expenses: the money actually
	// available to fund goals and grow net worth (deliberate savings plus
	// whatever is left over). Negative means the household is eating into its
	// savings. Every projection here is driven by this figure.
	SurplusCents int64
}

// DeductionRateBP is household deductions as basis points of gross.
func (o MonthlyOverview) DeductionRateBP() int64 {
	return rateBP(o.TotalDeductionsCents, o.TotalGrossIncomeCents)
}

// BuildOverview computes the monthly picture for the household.
func BuildOverview(in Inputs) MonthlyOverview {
	incomes := PartnerIncomes(in.Incomes, in.Rates, in.Display)
	ratio := SplitRatio(in.Household, incomes, in.PartnerA.ID, in.PartnerB.ID)

	ov := MonthlyOverview{Currency: in.Display, Ratio: ratio}
	ov.A = PartnerCashflow{UserID: in.PartnerA.ID, Name: in.PartnerA.Name, ShareRatio: ratio.A}
	ov.B = PartnerCashflow{UserID: in.PartnerB.ID, Name: in.PartnerB.Name, ShareRatio: ratio.B}

	for _, p := range []*PartnerCashflow{&ov.A, &ov.B} {
		inc := incomes[p.UserID]
		p.FixedIncomeCents = inc.FixedMonthlyCents
		p.VariableIncomeCents = inc.VariableMonthlyCents
		p.NetIncomeCents = inc.TotalMonthlyCents
		p.GrossIncomeCents = inc.GrossMonthlyCents
		p.DeductionsCents = inc.DeductionsMonthlyCents
	}

	for _, e := range in.Expenses {
		amt := in.amount(e.AmountCents, e.Currency)
		if e.Category == "personal" {
			p := &ov.A
			if e.UserID != nil && *e.UserID == ov.B.UserID {
				p = &ov.B
			} else if e.UserID == nil || *e.UserID != ov.A.UserID {
				continue // orphan personal expense
			}
			if e.IsSavings() {
				p.PersonalSavingsCents += amt
				ov.PersonalSavingsCents += amt
			} else {
				p.PersonalExpensesCents += amt
				ov.PersonalExpensesCents += amt
			}
			continue
		}
		a, b := ShareOf(amt, ratio)
		ov.A.CommonShareCents += a
		ov.B.CommonShareCents += b
		if e.IsSavings() {
			ov.CommonSavingsCents += amt
			ov.A.CommonSavingsShareCents += a
			ov.B.CommonSavingsShareCents += b
		} else {
			ov.CommonExpensesCents += amt
		}
	}

	for _, p := range []*PartnerCashflow{&ov.A, &ov.B} {
		p.TotalOutCents = p.PersonalExpensesCents + p.PersonalSavingsCents + p.CommonShareCents
		p.AvailableCents = p.NetIncomeCents - p.TotalOutCents
		p.TotalSavingsCents = p.PersonalSavingsCents + p.CommonSavingsShareCents
	}

	ov.TotalIncomeCents = ov.A.NetIncomeCents + ov.B.NetIncomeCents
	ov.TotalGrossIncomeCents = ov.A.GrossIncomeCents + ov.B.GrossIncomeCents
	ov.TotalDeductionsCents = ov.A.DeductionsCents + ov.B.DeductionsCents
	ov.TotalExpensesCents = ov.A.TotalOutCents + ov.B.TotalOutCents
	ov.TotalSavingsCents = ov.A.TotalSavingsCents + ov.B.TotalSavingsCents
	ov.AvailableCents = ov.A.AvailableCents + ov.B.AvailableCents
	ov.SurplusCents = ov.TotalSavingsCents + ov.AvailableCents
	return ov
}
