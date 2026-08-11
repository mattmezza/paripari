package service

import (
	"testing"

	"github.com/mattmezza/paripari/internal/model"
)

func change(kind string, target *int64, cents *int64, num *float64, label, text, cur string) model.ScenarioChange {
	return model.ScenarioChange{
		ChangeType: kind, TargetID: target, ValueCents: cents, ValueNum: num,
		Label: label, ValueText: text, Currency: cur,
	}
}

func TestApply(t *testing.T) {
	base := baseInputs()

	t.Run("expense_amount", func(t *testing.T) {
		got := Apply(base, []model.ScenarioChange{change("expense_amount", ptr(int64(20)), ptr(int64(350000)), nil, "", "", "")})
		if got.Expenses[0].AmountCents != 350000 {
			t.Errorf("amount = %d", got.Expenses[0].AmountCents)
		}
		if base.Expenses[0].AmountCents != 300000 {
			t.Errorf("baseline was mutated")
		}
	})

	t.Run("expense_add common", func(t *testing.T) {
		got := Apply(base, []model.ScenarioChange{change("expense_add", nil, ptr(int64(40000)), nil, "Car lease", "transport", "")})
		e := got.Expenses[len(got.Expenses)-1]
		if e.Name != "Car lease" || e.AmountCents != 40000 || e.Category != "common" || e.Currency != "CHF" || e.IsSavings {
			t.Errorf("added expense = %+v", e)
		}
	})

	t.Run("expense_add personal savings", func(t *testing.T) {
		got := Apply(base, []model.ScenarioChange{change("expense_add", ptr(int64(2)), ptr(int64(40000)), nil, "3a", "savings", "EUR")})
		e := got.Expenses[len(got.Expenses)-1]
		if e.Category != "personal" || e.UserID == nil || *e.UserID != 2 || !e.IsSavings || e.Currency != "EUR" {
			t.Errorf("added expense = %+v", e)
		}
	})

	t.Run("expense_remove", func(t *testing.T) {
		got := Apply(base, []model.ScenarioChange{change("expense_remove", ptr(int64(20)), nil, nil, "", "", "")})
		if len(got.Expenses) != len(base.Expenses)-1 {
			t.Fatalf("expenses = %d", len(got.Expenses))
		}
		for _, e := range got.Expenses {
			if e.ID == 20 {
				t.Errorf("expense 20 still present")
			}
		}
	})

	t.Run("income_amount is gross yearly", func(t *testing.T) {
		got := Apply(base, []model.ScenarioChange{change("income_amount", ptr(int64(10)), ptr(int64(12000000)), nil, "", "", "")})
		if got.Incomes[0].GrossYearlyCents != 12000000 {
			t.Errorf("gross = %d", got.Incomes[0].GrossYearlyCents)
		}
	})

	t.Run("income_add and income_remove", func(t *testing.T) {
		got := Apply(base, []model.ScenarioChange{
			change("income_add", ptr(int64(1)), ptr(int64(1200000)), nil, "Freelance", "variable", "EUR"),
			change("income_remove", ptr(int64(11)), nil, nil, "", "", ""),
		})
		if len(got.Incomes) != 2 {
			t.Fatalf("incomes = %d", len(got.Incomes))
		}
		added := got.Incomes[len(got.Incomes)-1]
		if added.UserID != 1 || added.Kind != "variable" || added.Currency != "EUR" || added.PayStructure != 12 {
			t.Errorf("added income = %+v", added)
		}
	})

	t.Run("return_rate", func(t *testing.T) {
		got := Apply(base, []model.ScenarioChange{change("return_rate", nil, nil, ptr(0.07), "", "", "")})
		if got.AnnualReturnRate != 0.07 {
			t.Errorf("rate = %v", got.AnnualReturnRate)
		}
	})

	t.Run("asset_add and asset_remove", func(t *testing.T) {
		withAsset := Apply(base, []model.ScenarioChange{change("asset_add", nil, ptr(int64(90000000)), nil, "Flat", "", "EUR")})
		if len(withAsset.Assets) != 1 || withAsset.Assets[0].EstimatedValueCents != 90000000 {
			t.Fatalf("assets = %+v", withAsset.Assets)
		}
		withAsset.Assets[0].ID = 50
		gone := Apply(withAsset, []model.ScenarioChange{change("asset_remove", ptr(int64(50)), nil, nil, "", "", "")})
		if len(gone.Assets) != 0 {
			t.Errorf("asset not removed: %+v", gone.Assets)
		}
	})

	t.Run("unknown change type is ignored", func(t *testing.T) {
		got := Apply(base, []model.ScenarioChange{change("nonsense", nil, ptr(int64(1)), nil, "", "", "")})
		if len(got.Expenses) != len(base.Expenses) || len(got.Incomes) != len(base.Incomes) {
			t.Errorf("unknown change altered inputs")
		}
	})
}

// prompt.md's headline example: a CHF 500 raise for partner A, CHF 500 more
// rent, and CHF 200 less school fees, stacked into one scenario.
func TestCompareStackedChanges(t *testing.T) {
	base := baseInputs()
	school := personalExp(24, 1, 30000, "CHF", false)
	base.Expenses = append(base.Expenses, school)

	cmp := Compare(base, []model.ScenarioChange{
		// +500/month net for A = +6'000/year gross-equivalent on a no-deduction source.
		change("income_amount", ptr(int64(10)), ptr(int64(895400*12+600000)), nil, "Raise", "", ""),
		change("expense_amount", ptr(int64(20)), ptr(int64(350000)), nil, "Rent up", "", ""),
		change("expense_amount", ptr(int64(24)), ptr(int64(10000)), nil, "Cheaper school", "", ""),
	})

	if got := cmp.Scenario.Overview.A.NetIncomeCents; got != 945400 {
		t.Errorf("A net income = %d, want 945400", got)
	}
	if got := cmp.Deltas.TotalIncomeCents; got != 50000 {
		t.Errorf("income delta = %d, want 50000", got)
	}
	// Net monthly effect: +500 income, +500 rent, -200 school = +200 surplus.
	if got := cmp.Scenario.Overview.SurplusCents - cmp.Current.Overview.SurplusCents; got != 20000 {
		t.Errorf("surplus delta = %d, want 20000", got)
	}
	if got := cmp.Deltas.AvailableCents; got != 20000 {
		t.Errorf("available delta = %d, want 20000", got)
	}
	// A earns relatively more, so A's share of common expenses rises.
	if cmp.Deltas.RatioA <= 0 || cmp.Deltas.RatioB >= 0 {
		t.Errorf("ratio deltas = %v/%v, want A up B down", cmp.Deltas.RatioA, cmp.Deltas.RatioB)
	}
	// Transfers must move by the rent increase in total.
	var transferDelta int64
	for _, d := range cmp.Deltas.Transfers {
		transferDelta += d
	}
	if transferDelta != 50000 {
		t.Errorf("transfer delta total = %d, want 50000", transferDelta)
	}
	// More surplus = the goal arrives no later, and net worth is higher everywhere.
	if cmp.Deltas.GoalETAMonths[40] > 0 {
		t.Errorf("goal ETA delta = %d, expected <= 0", cmp.Deltas.GoalETAMonths[40])
	}
	for _, y := range ProjectionYears {
		if cmp.Deltas.NetWorth[y] <= 0 {
			t.Errorf("net worth delta at %dy = %d, want positive", y, cmp.Deltas.NetWorth[y])
		}
	}
}

func TestCompareNoChangesIsIdentity(t *testing.T) {
	cmp := Compare(baseInputs(), nil)
	d := cmp.Deltas
	if d.AvailableCents != 0 || d.TotalSavingsCents != 0 || d.RatioA != 0 {
		t.Errorf("empty scenario produced deltas: %+v", d)
	}
	for k, v := range d.Transfers {
		if v != 0 {
			t.Errorf("transfer %s delta = %d", k, v)
		}
	}
	for _, y := range ProjectionYears {
		if d.NetWorth[y] != 0 {
			t.Errorf("net worth delta at %dy = %d", y, d.NetWorth[y])
		}
	}
	if d.GoalETAMonths[40] != 0 {
		t.Errorf("goal ETA delta = %d", d.GoalETAMonths[40])
	}
}

func TestCompareGoalBecomesUnreachable(t *testing.T) {
	cmp := Compare(baseInputs(), []model.ScenarioChange{
		change("expense_add", nil, ptr(int64(5000000)), nil, "Yacht", "transport", ""),
	})
	if cmp.Scenario.Overview.SurplusCents >= 0 {
		t.Fatalf("expected a negative surplus, got %d", cmp.Scenario.Overview.SurplusCents)
	}
	if cmp.Deltas.GoalETAFlip[40] != "unreachable" {
		t.Errorf("flip = %q, want unreachable", cmp.Deltas.GoalETAFlip[40])
	}
	if _, ok := cmp.Deltas.GoalETAMonths[40]; ok {
		t.Errorf("unreachable goals must not report a month delta")
	}
}

func TestCompareTransferRowDropped(t *testing.T) {
	cmp := Compare(baseInputs(), []model.ScenarioChange{
		change("expense_remove", ptr(int64(21)), nil, nil, "", "", ""), // the common savings expense
	})
	if got := cmp.Deltas.Transfers["1|Common savings"]; got != -44474 {
		t.Errorf("dropped row delta = %d, want -44474", got)
	}
	if got := cmp.Deltas.Transfers["2|Common savings"]; got != -55526 {
		t.Errorf("dropped row delta = %d, want -55526", got)
	}
}

func TestEvaluate(t *testing.T) {
	s := Evaluate(baseInputs())
	if s.NetWorth.TotalCents != 2500000 {
		t.Errorf("net worth = %d, want 2500000", s.NetWorth.TotalCents)
	}
	if len(s.Series) != ProjectionYears[len(ProjectionYears)-1]*12+1 {
		t.Errorf("series length = %d", len(s.Series))
	}
	if s.Series[0].BalanceCents != s.NetWorth.TotalCents {
		t.Errorf("series must start at current net worth")
	}
	if len(s.Goals) != 1 || s.Goals[0].ETAMonths <= 0 {
		t.Errorf("goals = %+v", s.Goals)
	}
	if s.Projection[1] <= s.NetWorth.TotalCents {
		t.Errorf("1y projection %d should exceed today %d", s.Projection[1], s.NetWorth.TotalCents)
	}
}
