package service

import "github.com/mattmezza/paripari/internal/model"

// TripTotals is a trip plan costed out in the display currency.
type TripTotals struct {
	Trip         model.TripPlan
	Currency     string
	TotalCents   int64
	MonthsToSave int
	// MonthlyCents is what the household must set aside each month to fund the
	// trip in MonthsToSave months.
	MonthlyCents int64
	ByCategory   map[string]int64
}

// TripTotal sums a trip's items into the display currency.
func TripTotal(t model.TripPlan, rates Rates, display string) TripTotals {
	tt := TripTotals{Trip: t, Currency: display, ByCategory: map[string]int64{}}
	for _, it := range t.Items {
		v := conv(rates, it.AmountCents, it.Currency, display)
		tt.TotalCents += v
		tt.ByCategory[it.Category] += v
	}
	tt.MonthsToSave = t.MonthsToSave
	if tt.MonthsToSave < 1 {
		tt.MonthsToSave = 1
	}
	tt.MonthlyCents = divRound(tt.TotalCents, int64(tt.MonthsToSave))
	return tt
}

// TripChange expresses the trip as a temporary common expense, so the scenario
// engine can price it like any other change.
//
// It is deliberately NOT tagged as savings: trip money is consumed, not
// accumulated, so it must reduce the household surplus (and therefore push goal
// ETAs out) rather than count as progress.
func (tt TripTotals) TripChange() model.ScenarioChange {
	amt := tt.MonthlyCents
	return model.ScenarioChange{
		ChangeType: "expense_add",
		Label:      "Trip: " + tt.Trip.Name,
		ValueCents: &amt,
		ValueText:  "trip",
		Currency:   tt.Currency,
	}
}

// TripImpact answers "with this trip vs without": the monthly saving becomes a
// common savings expense, and every downstream number (available cash,
// transfers, goal ETAs, net worth) is recomputed.
func TripImpact(in Inputs, t model.TripPlan) (TripTotals, Comparison) {
	tt := TripTotal(t, in.Rates, in.Display)
	return tt, Compare(in, []model.ScenarioChange{tt.TripChange()})
}
