package service

import "github.com/mattmezza/paripari/internal/model"

// ProjectionYears are the net-worth horizons every comparison reports.
var ProjectionYears = []int{1, 5, 10, 20}

// Apply returns a copy of `in` with the scenario changes applied, in order.
// Slices are copied, so the baseline is never mutated.
//
// Change semantics (matching the scenario_changes CHECK constraint):
//
//	expense_amount  TargetID=expense id,      ValueCents=new monthly amount
//	expense_add     Label=name, ValueCents=monthly amount, Currency,
//	                ValueText=subcategory ("savings" tags it as savings),
//	                TargetID=user id for a personal expense (nil = common)
//	expense_remove  TargetID=expense id
//	income_amount   TargetID=income source id, ValueCents=new GROSS YEARLY
//	income_add      TargetID=user id, Label=name, ValueCents=gross yearly,
//	                Currency, ValueText=kind ("variable"|"fixed")
//	income_remove   TargetID=income source id
//	return_rate     ValueNum=new annual rate (0.05 = 5%)
//	asset_add       Label=name, ValueCents=value, Currency
//	asset_remove    TargetID=asset id
func Apply(in Inputs, changes []model.ScenarioChange) Inputs {
	out := in
	out.Expenses = append([]model.Expense(nil), in.Expenses...)
	out.Incomes = append([]model.IncomeSource(nil), in.Incomes...)
	out.Assets = append([]model.Asset(nil), in.Assets...)

	for _, c := range changes {
		switch c.ChangeType {
		case "expense_amount":
			for i := range out.Expenses {
				if c.TargetID != nil && out.Expenses[i].ID == *c.TargetID && c.ValueCents != nil {
					out.Expenses[i].AmountCents = *c.ValueCents
				}
			}
		case "expense_add":
			e := model.Expense{
				HouseholdID: in.Household.ID, Name: c.Label,
				Currency: orDefault(c.Currency, in.Display),
				Category: "common", Subcategory: c.ValueText,
				Kind: model.KindForSubcategory("", c.ValueText),
			}
			if c.ValueCents != nil {
				e.AmountCents = *c.ValueCents
			}
			if c.TargetID != nil {
				e.Category, e.UserID = "personal", c.TargetID
			}
			out.Expenses = append(out.Expenses, e)
		case "expense_remove":
			out.Expenses = filterExpenses(out.Expenses, c.TargetID)
		case "income_amount":
			for i := range out.Incomes {
				if c.TargetID != nil && out.Incomes[i].ID == *c.TargetID && c.ValueCents != nil {
					out.Incomes[i].GrossYearlyCents = *c.ValueCents
				}
			}
		case "income_add":
			s := model.IncomeSource{
				HouseholdID: in.Household.ID, Name: c.Label,
				Kind: orDefault(c.ValueText, "fixed"), PayStructure: 12,
				Currency: orDefault(c.Currency, in.Display),
			}
			if c.TargetID != nil {
				s.UserID = *c.TargetID
			}
			if c.ValueCents != nil {
				s.GrossYearlyCents = *c.ValueCents
			}
			out.Incomes = append(out.Incomes, s)
		case "income_remove":
			kept := out.Incomes[:0:0]
			for _, s := range out.Incomes {
				if c.TargetID == nil || s.ID != *c.TargetID {
					kept = append(kept, s)
				}
			}
			out.Incomes = kept
		case "return_rate":
			if c.ValueNum != nil {
				out.AnnualReturnRate = *c.ValueNum
			}
		case "asset_add":
			a := model.Asset{
				HouseholdID: in.Household.ID, Name: c.Label, Kind: "real_estate",
				Currency: orDefault(c.Currency, in.Display),
			}
			if c.ValueCents != nil {
				a.EstimatedValueCents = *c.ValueCents
			}
			out.Assets = append(out.Assets, a)
		case "asset_remove":
			kept := out.Assets[:0:0]
			for _, a := range out.Assets {
				if c.TargetID == nil || a.ID != *c.TargetID {
					kept = append(kept, a)
				}
			}
			out.Assets = kept
		}
	}
	return out
}

func filterExpenses(list []model.Expense, id *int64) []model.Expense {
	kept := list[:0:0]
	for _, e := range list {
		if id == nil || e.ID != *id {
			kept = append(kept, e)
		}
	}
	return kept
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Snapshot is the full computed picture for one set of Inputs.
type Snapshot struct {
	Overview  MonthlyOverview
	Transfers TransferPlan
	NetWorth  NetWorth
	Goals     []GoalProgress
	// Projection is total net worth at each ProjectionYears mark.
	Projection map[int]int64
	// Series is the monthly net-worth curve over the longest horizon.
	Series []Point
}

// Evaluate computes everything the app shows for one set of Inputs.
func Evaluate(in Inputs) Snapshot {
	ov := BuildOverview(in)
	nw := ComputeNetWorth(in)
	months := ProjectionYears[len(ProjectionYears)-1] * 12
	return Snapshot{
		Overview:   ov,
		Transfers:  BuildTransfers(in, ov),
		NetWorth:   nw,
		Goals:      GoalProgresses(in, ov),
		Projection: ProjectNetWorth(nw.TotalCents, ov.SurplusCents, in.AnnualReturnRate, ProjectionYears),
		Series:     ProjectSavings(nw.TotalCents, ov.SurplusCents, in.AnnualReturnRate, months),
	}
}

// Deltas is scenario minus current. Positive means the scenario is higher;
// for GoalETAMonths, negative means the goal arrives sooner.
type Deltas struct {
	AvailableCents     int64
	TotalIncomeCents   int64
	TotalExpensesCents int64
	TotalSavingsCents  int64
	RatioA, RatioB     float64
	AAvailableCents    int64
	BAvailableCents    int64
	// Transfers is keyed "<fromUserID>|<to account name>".
	Transfers map[string]int64
	// NetWorth is keyed by year (1/5/10/20).
	NetWorth map[int]int64
	// GoalETAMonths is keyed by goal id. A value is only present when both
	// sides are reachable; unreachable-on-either-side goals land in GoalETAFlip.
	GoalETAMonths map[int64]int
	GoalETAFlip   map[int64]string // "unreachable" | "reachable"
}

// Comparison is the what-if result: both sides plus the diff.
type Comparison struct {
	Current  Snapshot
	Scenario Snapshot
	Deltas   Deltas
}

// Compare applies the changes to the baseline and diffs everything.
func Compare(baseline Inputs, changes []model.ScenarioChange) Comparison {
	cur := Evaluate(baseline)
	sc := Evaluate(Apply(baseline, changes))
	return Comparison{Current: cur, Scenario: sc, Deltas: diff(cur, sc)}
}

func diff(cur, sc Snapshot) Deltas {
	d := Deltas{
		AvailableCents:     sc.Overview.AvailableCents - cur.Overview.AvailableCents,
		TotalIncomeCents:   sc.Overview.TotalIncomeCents - cur.Overview.TotalIncomeCents,
		TotalExpensesCents: sc.Overview.TotalExpensesCents - cur.Overview.TotalExpensesCents,
		TotalSavingsCents:  sc.Overview.TotalSavingsCents - cur.Overview.TotalSavingsCents,
		RatioA:             sc.Overview.Ratio.A - cur.Overview.Ratio.A,
		RatioB:             sc.Overview.Ratio.B - cur.Overview.Ratio.B,
		AAvailableCents:    sc.Overview.A.AvailableCents - cur.Overview.A.AvailableCents,
		BAvailableCents:    sc.Overview.B.AvailableCents - cur.Overview.B.AvailableCents,
		Transfers:          map[string]int64{},
		NetWorth:           map[int]int64{},
		GoalETAMonths:      map[int64]int{},
		GoalETAFlip:        map[int64]string{},
	}
	base := map[string]int64{}
	for _, t := range cur.Transfers.Rows {
		base[t.key()] += t.AmountCents
	}
	for _, t := range sc.Transfers.Rows {
		d.Transfers[t.key()] = t.AmountCents - base[t.key()]
		delete(base, t.key())
	}
	for k, v := range base { // rows the scenario drops
		d.Transfers[k] = -v
	}
	for _, y := range ProjectionYears {
		d.NetWorth[y] = sc.Projection[y] - cur.Projection[y]
	}
	curETA := map[int64]int{}
	for _, g := range cur.Goals {
		curETA[g.Goal.ID] = g.ETAMonths
	}
	for _, g := range sc.Goals {
		before, ok := curETA[g.Goal.ID]
		if !ok {
			continue
		}
		switch {
		case before >= 0 && g.ETAMonths >= 0:
			d.GoalETAMonths[g.Goal.ID] = g.ETAMonths - before
		case before >= 0:
			d.GoalETAFlip[g.Goal.ID] = "unreachable"
		case g.ETAMonths >= 0:
			d.GoalETAFlip[g.Goal.ID] = "reachable"
		}
	}
	return d
}
