package service

import (
	"testing"

	"github.com/mattmezza/paripari/internal/model"
)

func trip(months int, items ...model.TripItem) model.TripPlan {
	return model.TripPlan{ID: 60, Name: "Japan", MonthsToSave: months, Items: items}
}

func item(name, cat string, cents int64, cur string) model.TripItem {
	return model.TripItem{Name: name, Category: cat, AmountCents: cents, Currency: cur}
}

func TestTripTotal(t *testing.T) {
	tests := []struct {
		name        string
		plan        model.TripPlan
		wantTotal   int64
		wantMonths  int
		wantMonthly int64
	}{
		{
			name:      "single currency",
			plan:      trip(10, item("Flights", "transport", 200000, "CHF"), item("Hotel", "stay", 300000, "CHF")),
			wantTotal: 500000, wantMonths: 10, wantMonthly: 50000,
		},
		{
			name:      "multi currency converted",
			plan:      trip(4, item("Flights", "transport", 100000, "EUR"), item("Hotel", "stay", 100000, "USD")),
			wantTotal: 95000 + 90000, wantMonths: 4, wantMonthly: 46250,
		},
		{
			name:      "rounding of an indivisible total",
			plan:      trip(3, item("All in", "misc", 100000, "CHF")),
			wantTotal: 100000, wantMonths: 3, wantMonthly: 33333,
		},
		{
			name:      "zero months clamps to one",
			plan:      trip(0, item("All in", "misc", 100000, "CHF")),
			wantTotal: 100000, wantMonths: 1, wantMonthly: 100000,
		},
		{name: "empty trip", plan: trip(6), wantTotal: 0, wantMonths: 6, wantMonthly: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TripTotal(tc.plan, testRates, "CHF")
			if got.TotalCents != tc.wantTotal {
				t.Errorf("total = %d, want %d", got.TotalCents, tc.wantTotal)
			}
			if got.MonthsToSave != tc.wantMonths {
				t.Errorf("months = %d, want %d", got.MonthsToSave, tc.wantMonths)
			}
			if got.MonthlyCents != tc.wantMonthly {
				t.Errorf("monthly = %d, want %d", got.MonthlyCents, tc.wantMonthly)
			}
			if got.Currency != "CHF" {
				t.Errorf("currency = %q", got.Currency)
			}
		})
	}
}

func TestTripTotalByCategory(t *testing.T) {
	got := TripTotal(trip(2,
		item("Flight out", "transport", 100000, "CHF"),
		item("Flight back", "transport", 120000, "CHF"),
		item("Ryokan", "stay", 300000, "CHF"),
	), testRates, "CHF")
	if got.ByCategory["transport"] != 220000 || got.ByCategory["stay"] != 300000 {
		t.Errorf("by category = %+v", got.ByCategory)
	}
}

func TestTripImpact(t *testing.T) {
	in := baseInputs()
	in.Goals[0].TargetAmountCents = 100000000 // CHF 1M, far enough out to show a delay
	tt, cmp := TripImpact(in, trip(10, item("All in", "misc", 1000000, "CHF")))

	if tt.MonthlyCents != 100000 {
		t.Fatalf("monthly = %d, want 100000", tt.MonthlyCents)
	}
	// The trip eats CHF 1'000/month of surplus...
	if got := cmp.Deltas.AvailableCents; got != -100000 {
		t.Errorf("available delta = %d, want -100000", got)
	}
	// ...and is NOT counted as savings progress.
	if cmp.Deltas.TotalSavingsCents != 0 {
		t.Errorf("savings delta = %d, want 0", cmp.Deltas.TotalSavingsCents)
	}
	// ...so goals arrive later and net worth is lower at every horizon.
	if cmp.Deltas.GoalETAMonths[40] <= 0 {
		t.Errorf("goal ETA delta = %d, want a delay", cmp.Deltas.GoalETAMonths[40])
	}
	for _, y := range ProjectionYears {
		if cmp.Deltas.NetWorth[y] >= 0 {
			t.Errorf("net worth delta at %dy = %d, want negative", y, cmp.Deltas.NetWorth[y])
		}
	}
	// The trip is a common expense, so both partners' transfers go up.
	if cmp.Deltas.Transfers["1|Common checking"] <= 0 || cmp.Deltas.Transfers["2|Common checking"] <= 0 {
		t.Errorf("transfer deltas = %+v", cmp.Deltas.Transfers)
	}
}

func TestTripChangeIsNotSavings(t *testing.T) {
	tt := TripTotal(trip(5, item("All in", "misc", 500000, "CHF")), testRates, "CHF")
	c := tt.TripChange()
	if c.ChangeType != "expense_add" || c.ValueText == "savings" || *c.ValueCents != 100000 {
		t.Errorf("trip change = %+v", c)
	}
	applied := Apply(baseInputs(), []model.ScenarioChange{c})
	e := applied.Expenses[len(applied.Expenses)-1]
	if e.IsSavings || e.Category != "common" || e.Name != "Trip: Japan" {
		t.Errorf("applied trip expense = %+v", e)
	}
}
