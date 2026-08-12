package service

import "github.com/mattmezza/paripari/internal/model"

// TripTotals is a trip plan costed out in the display currency.
type TripTotals struct {
	Trip         model.TripPlan
	Currency     string
	TotalCents   int64
	MonthsToSave int
	// MonthlyCents is what the household must set aside each month to fund the
	// trip in MonthsToSave months. Zero for a one-shot trip: nothing recurring.
	MonthlyCents int64
	ByCategory   map[string]int64
	// OneShot means the trip is paid out of what its account already holds,
	// rather than saved up month by month.
	OneShot bool
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
	if tt.OneShot = t.IsOneShot(); tt.OneShot {
		// The money is already saved: it leaves the account in one payment, so
		// there is nothing to set aside each month. MonthsToSave is kept as the
		// user last left it, so switching back to spread funding remembers it.
		return tt
	}
	tt.MonthlyCents = divRound(tt.TotalCents, int64(tt.MonthsToSave))
	return tt
}

// TripFunding is the account behind a trip and, for one-shot funding, whether
// it actually holds enough.
type TripFunding struct {
	// Account is the account named by the plan, or the household's common
	// checking account when it names none — the same default an unassigned
	// common expense follows in BuildTransfers. nil when neither exists.
	Account        *model.Account
	BalanceCents   int64 // Account's balance in the display currency
	ShortfallCents int64 // what a one-shot trip costs beyond that balance
}

// FundingOf resolves where a trip's money comes from.
func FundingOf(in Inputs, tt TripTotals) TripFunding {
	var f TripFunding
	want := tt.Trip.FundingAccountID
	for i, a := range in.Accounts {
		if (want != nil && a.ID == *want) || (want == nil && a.UserID == nil && a.Purpose == "checking") {
			f.Account = &in.Accounts[i]
			f.BalanceCents = in.amount(a.BalanceCents, a.Currency)
			break
		}
	}
	if tt.OneShot && tt.TotalCents > f.BalanceCents {
		f.ShortfallCents = tt.TotalCents - f.BalanceCents
	}
	return f
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
//
// A one-shot trip changes no monthly figure at all — it is paid from money the
// household already has — so the scenario is the same cashflow starting from a
// drawn-down account. That is the honest reading: the surplus is untouched, the
// net worth line simply starts lower and never catches up.
func TripImpact(in Inputs, t model.TripPlan) (TripTotals, Comparison) {
	tt := TripTotal(t, in.Rates, in.Display)
	if !tt.OneShot {
		return tt, Compare(in, []model.ScenarioChange{tt.TripChange()})
	}
	// Compare applies scenario changes, and no change type moves a balance, so
	// the two sides are evaluated by hand: today's household against the same
	// household with the trip already paid for.
	cur, sc := Evaluate(in), Evaluate(drawDown(in, tt))
	return tt, Comparison{Current: cur, Scenario: sc, Deltas: diff(cur, sc)}
}

// drawDown returns in with the trip's funding account lightened by the trip's
// total. ponytail: a household with no account at all has nothing to draw from,
// so the scenario is unchanged — the shortfall warning on the trip screen is
// what tells that story.
func drawDown(in Inputs, tt TripTotals) Inputs {
	f := FundingOf(in, tt)
	if f.Account == nil {
		return in
	}
	out := in
	out.Accounts = append([]model.Account(nil), in.Accounts...)
	for i := range out.Accounts {
		if out.Accounts[i].ID == f.Account.ID {
			out.Accounts[i].BalanceCents -= conv(in.Rates, tt.TotalCents, in.Display, out.Accounts[i].Currency)
			break
		}
	}
	return out
}
