package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// registerScenarios mounts the what-if scenario builder + comparison, the
// killer feature: named stacks of parameter changes diffed against the
// household's real numbers.
func registerScenarios(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /scenarios", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		scenarios, err := d.Store.Scenarios(in.Household.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "scenarios", &view.PageData{
			Title: "Scenarios", Description: "What-if scenarios for your household finances.",
			Data: scenarioListData(scenarios, in),
		})
	})

	mux.HandleFunc("POST /scenarios", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		name := r.PostFormValue("name")
		if name == "" {
			name = "Untitled scenario"
		}
		id, err := d.Store.CreateScenario(&model.Scenario{HouseholdID: in.Household.ID, Name: name})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirect(w, r, fmt.Sprintf("/scenarios/%d", id))
	})

	mux.HandleFunc("GET /scenarios/{id}", func(w http.ResponseWriter, r *http.Request) {
		in, sc, err := loadScenario(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		all, err := d.Store.Scenarios(in.Household.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "scenario-detail", &view.PageData{
			Title: sc.Name, Description: "Scenario builder and comparison.",
			Data: scenarioDetailData(d, in, *sc, all, r),
		})
	})

	mux.HandleFunc("POST /scenarios/{id}", func(w http.ResponseWriter, r *http.Request) {
		in, sc, err := loadScenario(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		sc.Name = r.PostFormValue("name")
		sc.Description = r.PostFormValue("description")
		if sc.Name == "" {
			sc.Name = "Untitled scenario"
		}
		if err := d.Store.UpdateScenario(sc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		all, _ := d.Store.Scenarios(in.Household.ID)
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/scenarios-builder", scenarioDetailData(d, in, *sc, all, r))
	})

	mux.HandleFunc("DELETE /scenarios/{id}", func(w http.ResponseWriter, r *http.Request) {
		in, sc, err := loadScenario(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if err := d.Store.DeleteScenario(in.Household.ID, sc.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirect(w, r, "/scenarios")
	})

	mux.HandleFunc("POST /scenarios/{id}/changes", func(w http.ResponseWriter, r *http.Request) {
		in, sc, err := loadScenario(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		c, err := parseChangeForm(r, in)
		if err != nil {
			all, _ := d.Store.Scenarios(in.Household.ID)
			data := scenarioDetailData(d, in, *sc, all, r)
			data.FormError = err.Error()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			d.View.Partial(w, "partials/scenarios-builder", data)
			return
		}
		c.ScenarioID = sc.ID
		if _, err := d.Store.CreateScenarioChange(in.Household.ID, &c); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sc2, err := d.Store.Scenario(in.Household.ID, sc.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		all, _ := d.Store.Scenarios(in.Household.ID)
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/scenarios-builder", scenarioDetailData(d, in, *sc2, all, r))
	})

	mux.HandleFunc("DELETE /scenarios/{id}/changes/{changeID}", func(w http.ResponseWriter, r *http.Request) {
		in, sc, err := loadScenario(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		changeID, _ := strconv.ParseInt(r.PathValue("changeID"), 10, 64)
		if err := d.Store.DeleteScenarioChange(in.Household.ID, changeID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sc2, err := d.Store.Scenario(in.Household.ID, sc.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		all, _ := d.Store.Scenarios(in.Household.ID)
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/scenarios-builder", scenarioDetailData(d, in, *sc2, all, r))
	})

	mux.HandleFunc("GET /scenarios/{id}/compare", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("HX-Request") != "true" {
			// Direct navigation: the compare region lives inside the scenario page.
			http.Redirect(w, r, "/scenarios/"+r.PathValue("id")+"?"+r.URL.RawQuery, http.StatusSeeOther)
			return
		}
		in, sc, err := loadScenario(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		all, err := d.Store.Scenarios(in.Household.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.View.Partial(w, "partials/scenarios-compare", buildCompare(d, in, *sc, all, r))
	})
}

// loadScenario resolves the household + scenario for the {id} path segment.
func loadScenario(d *Deps, r *http.Request) (service.Inputs, *model.Scenario, error) {
	in, err := BuildInputs(d, r)
	if err != nil {
		return in, nil, err
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return in, nil, err
	}
	sc, err := d.Store.Scenario(in.Household.ID, id)
	if err != nil {
		return in, nil, err
	}
	return in, sc, nil
}

// ─── List page ──────────────────────────────────────────────────────────────

type scenarioCard struct {
	model.Scenario
	ChangeCount int
	DeltaText   string
	DeltaTone   string
}

type scenarioListView struct {
	Cards    []scenarioCard
	Currency string
}

func scenarioListData(scenarios []model.Scenario, in service.Inputs) scenarioListView {
	base := service.Evaluate(in)
	cards := make([]scenarioCard, 0, len(scenarios))
	for _, sc := range scenarios {
		scIn := service.Apply(in, sc.Changes)
		scOv := service.BuildOverview(scIn)
		delta := scOv.SurplusCents - base.Overview.SurplusCents
		text, tone := formatSignedMoney(delta, in.Display)
		cards = append(cards, scenarioCard{Scenario: sc, ChangeCount: len(sc.Changes), DeltaText: text + "/mo", DeltaTone: tone})
	}
	return scenarioListView{Cards: cards, Currency: in.Display}
}

// ─── Builder page ───────────────────────────────────────────────────────────

type scenarioDetailView struct {
	Scenario   model.Scenario
	ChangeRows []changeRow
	Expenses   []model.Expense
	Incomes    []model.IncomeSource
	Assets     []model.Asset
	PartnerA   model.User
	PartnerB   model.User
	Currency   string
	Currencies []string
	FormError  string
	OtherScens []model.Scenario
	Compare    compareView
}

type changeRow struct {
	model.ScenarioChange
	Description string
}

func scenarioDetailData(d *Deps, in service.Inputs, sc model.Scenario, all []model.Scenario, r *http.Request) *scenarioDetailView {
	rows := make([]changeRow, 0, len(sc.Changes))
	for _, c := range sc.Changes {
		rows = append(rows, changeRow{ScenarioChange: c, Description: describeChange(c, in)})
	}
	var others []model.Scenario
	for _, o := range all {
		if o.ID != sc.ID {
			others = append(others, o)
		}
	}
	return &scenarioDetailView{
		Scenario: sc, ChangeRows: rows,
		Expenses: in.Expenses, Incomes: in.Incomes, Assets: in.Assets,
		PartnerA: in.PartnerA, PartnerB: in.PartnerB, Currency: in.Display, Currencies: view.Currencies,
		OtherScens: others,
		Compare:    buildCompare(d, in, sc, all, r),
	}
}

// describeChange renders a human-readable summary of one change, resolving
// target ids against the current household state.
func describeChange(c model.ScenarioChange, in service.Inputs) string {
	find := func(name string) string { return name }
	switch c.ChangeType {
	case "expense_amount":
		for _, e := range in.Expenses {
			if c.TargetID != nil && e.ID == *c.TargetID {
				return find(fmt.Sprintf("Set %q to %s/mo", e.Name, service.Money(deref(c.ValueCents), e.Currency)))
			}
		}
		return "Change an expense amount"
	case "expense_add":
		return fmt.Sprintf("Add expense %q, %s/mo", c.Label, service.Money(deref(c.ValueCents), orDefault2(c.Currency, in.Display)))
	case "expense_remove":
		for _, e := range in.Expenses {
			if c.TargetID != nil && e.ID == *c.TargetID {
				return fmt.Sprintf("Remove expense %q", e.Name)
			}
		}
		return "Remove an expense"
	case "income_amount":
		for _, s := range in.Incomes {
			if c.TargetID != nil && s.ID == *c.TargetID {
				return fmt.Sprintf("Set %q gross yearly to %s", s.Name, service.Money(deref(c.ValueCents), s.Currency))
			}
		}
		return "Change an income amount"
	case "income_add":
		return fmt.Sprintf("Add income %q, %s/yr gross", c.Label, service.Money(deref(c.ValueCents), orDefault2(c.Currency, in.Display)))
	case "income_remove":
		for _, s := range in.Incomes {
			if c.TargetID != nil && s.ID == *c.TargetID {
				return fmt.Sprintf("Remove income %q", s.Name)
			}
		}
		return "Remove an income source"
	case "return_rate":
		if c.ValueNum != nil {
			return fmt.Sprintf("Set annual return rate to %s", view.Pct(*c.ValueNum))
		}
		return "Change the return rate"
	case "asset_add":
		return fmt.Sprintf("Add asset %q, %s", c.Label, service.Money(deref(c.ValueCents), orDefault2(c.Currency, in.Display)))
	case "asset_remove":
		for _, a := range in.Assets {
			if c.TargetID != nil && a.ID == *c.TargetID {
				return fmt.Sprintf("Remove asset %q", a.Name)
			}
		}
		return "Remove an asset"
	}
	return c.ChangeType
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func orDefault2(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// parseChangeForm builds a model.ScenarioChange from POST form fields. The
// visible field set depends on change_type (see scenarios-builder.html); this
// only reads the ones that type needs.
func parseChangeForm(r *http.Request, in service.Inputs) (model.ScenarioChange, error) {
	ct := r.PostFormValue("change_type")
	c := model.ScenarioChange{ChangeType: ct}

	amount := func(field string) (int64, error) { return ParseAmount(r.PostFormValue(field)) }
	targetID := func(field string) (*int64, error) {
		v := r.PostFormValue(field)
		if v == "" {
			return nil, nil
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid target")
		}
		return &id, nil
	}

	switch ct {
	case "expense_amount":
		id, err := targetID("target_expense")
		if err != nil || id == nil {
			return c, fmt.Errorf("pick an expense")
		}
		cents, err := amount("amount")
		if err != nil {
			return c, err
		}
		c.TargetID, c.ValueCents = id, &cents

	case "expense_add":
		c.Label = r.PostFormValue("label")
		if c.Label == "" {
			return c, fmt.Errorf("name the new expense")
		}
		cents, err := amount("amount")
		if err != nil {
			return c, err
		}
		c.ValueCents = &cents
		c.Currency = orDefault2(r.PostFormValue("currency"), in.Display)
		c.ValueText = r.PostFormValue("subcategory")
		if id, err := targetID("target_user"); err == nil {
			c.TargetID = id
		}

	case "expense_remove":
		id, err := targetID("target_expense")
		if err != nil || id == nil {
			return c, fmt.Errorf("pick an expense")
		}
		c.TargetID = id

	case "income_amount":
		id, err := targetID("target_income")
		if err != nil || id == nil {
			return c, fmt.Errorf("pick an income source")
		}
		cents, err := amount("amount")
		if err != nil {
			return c, err
		}
		c.TargetID, c.ValueCents = id, &cents

	case "income_add":
		id, err := targetID("target_user")
		if err != nil || id == nil {
			return c, fmt.Errorf("pick a partner")
		}
		c.Label = r.PostFormValue("label")
		if c.Label == "" {
			return c, fmt.Errorf("name the new income")
		}
		cents, err := amount("amount")
		if err != nil {
			return c, err
		}
		c.TargetID, c.ValueCents = id, &cents
		c.Currency = orDefault2(r.PostFormValue("currency"), in.Display)
		c.ValueText = orDefault2(r.PostFormValue("kind"), "fixed")

	case "income_remove":
		id, err := targetID("target_income")
		if err != nil || id == nil {
			return c, fmt.Errorf("pick an income source")
		}
		c.TargetID = id

	case "return_rate":
		pct, err := strconv.ParseFloat(r.PostFormValue("rate_pct"), 64)
		if err != nil || pct < 0 || pct > 20 {
			return c, fmt.Errorf("enter a rate between 0 and 20%%")
		}
		rate := pct / 100
		c.ValueNum = &rate

	case "asset_add":
		c.Label = r.PostFormValue("label")
		if c.Label == "" {
			return c, fmt.Errorf("name the new asset")
		}
		cents, err := amount("amount")
		if err != nil {
			return c, err
		}
		c.ValueCents = &cents
		c.Currency = orDefault2(r.PostFormValue("currency"), in.Display)

	case "asset_remove":
		id, err := targetID("target_asset")
		if err != nil || id == nil {
			return c, fmt.Errorf("pick an asset")
		}
		c.TargetID = id

	default:
		return c, fmt.Errorf("unknown change type")
	}
	return c, nil
}

// ─── Comparison (the payoff) ────────────────────────────────────────────────

type metricRow struct {
	Label                 string
	Current, ScenA, ScenB string
	DeltaA, DeltaATone    string
	DeltaB, DeltaBTone    string
	HasB                  bool
}

type compareView struct {
	Scenario     model.Scenario
	ScenarioB    *model.Scenario
	OtherScens   []model.Scenario
	SelectedB    int64
	Currency     string
	Rows         []metricRow
	GoalRows     []metricRow
	TransferRows []metricRow
	Payload      projPayload
}

func buildCompare(d *Deps, in service.Inputs, sc model.Scenario, all []model.Scenario, r *http.Request) compareView {
	cmpA := service.Compare(in, sc.Changes)

	var scB *model.Scenario
	var cmpB *service.Comparison
	var selectedB int64
	if bStr := r.URL.Query().Get("b"); bStr != "" {
		if bID, err := strconv.ParseInt(bStr, 10, 64); err == nil {
			for i := range all {
				if all[i].ID == bID {
					scB = &all[i]
					c := service.Compare(in, scB.Changes)
					cmpB = &c
					selectedB = bID
					break
				}
			}
		}
	}

	var others []model.Scenario
	for _, o := range all {
		if o.ID != sc.ID {
			others = append(others, o)
		}
	}

	hasB := cmpB != nil
	cur := in.Display
	rows := []metricRow{
		moneyRow("Monthly surplus", cmpA.Current.Overview.SurplusCents, cmpA.Scenario.Overview.SurplusCents, cmpB, hasB, cur, func(s service.Snapshot) int64 { return s.Overview.SurplusCents }),
		moneyRow(cmpA.Current.Overview.A.Name+" available", cmpA.Current.Overview.A.AvailableCents, cmpA.Scenario.Overview.A.AvailableCents, cmpB, hasB, cur, func(s service.Snapshot) int64 { return s.Overview.A.AvailableCents }),
		moneyRow(cmpA.Current.Overview.B.Name+" available", cmpA.Current.Overview.B.AvailableCents, cmpA.Scenario.Overview.B.AvailableCents, cmpB, hasB, cur, func(s service.Snapshot) int64 { return s.Overview.B.AvailableCents }),
		ratioRow("Split ratio "+cmpA.Current.Overview.A.Name+" / "+cmpA.Current.Overview.B.Name, cmpA.Current.Overview.Ratio, cmpA.Scenario.Overview.Ratio, cmpB, hasB),
	}
	for _, y := range service.ProjectionYears {
		rows = append(rows, moneyRow(fmt.Sprintf("Net worth @ %dy", y), cmpA.Current.Projection[y], cmpA.Scenario.Projection[y], cmpB, hasB, cur,
			func(s service.Snapshot) int64 { return s.Projection[y] }))
	}

	var goalRows []metricRow
	curETA := map[int64]service.GoalProgress{}
	for _, g := range cmpA.Current.Goals {
		curETA[g.Goal.ID] = g
	}
	scanGoals := func(snap service.Snapshot) map[int64]service.GoalProgress {
		m := map[int64]service.GoalProgress{}
		for _, g := range snap.Goals {
			m[g.Goal.ID] = g
		}
		return m
	}
	aETA := scanGoals(cmpA.Scenario)
	var bETA map[int64]service.GoalProgress
	if hasB {
		bETA = scanGoals(cmpB.Scenario)
	}
	for _, g := range cmpA.Current.Goals {
		a := aETA[g.Goal.ID]
		row := metaEtaRow(g.Goal.Name, g, a)
		if hasB {
			b := bETA[g.Goal.ID]
			bText, bDelta, bTone := etaCell(g, b)
			row.ScenB, row.DeltaB, row.DeltaBTone, row.HasB = bText, bDelta, bTone, true
		}
		goalRows = append(goalRows, row)
	}

	var transferRows []metricRow
	base := map[string]int64{}
	for _, t := range cmpA.Current.Transfers.Rows {
		base[t.From+" -> "+t.To] = t.AmountCents
	}
	seen := map[string]bool{}
	for _, t := range cmpA.Scenario.Transfers.Rows {
		key := t.From + " -> " + t.To
		seen[key] = true
		before := base[key]
		text, tone := formatSignedMoney(t.AmountCents-before, cur)
		transferRows = append(transferRows, metricRow{
			Label: key, Current: service.Money(before, cur), ScenA: service.Money(t.AmountCents, cur),
			DeltaA: text, DeltaATone: tone,
		})
	}
	for key, before := range base {
		if seen[key] || before == 0 {
			continue
		}
		text, tone := formatSignedMoney(-before, cur)
		transferRows = append(transferRows, metricRow{Label: key, Current: service.Money(before, cur), ScenA: "removed", DeltaA: text, DeltaATone: tone})
	}

	months := service.ProjectionYears[len(service.ProjectionYears)-1] * 12
	series := []chartSeries{
		{Name: "Current", Points: cmpA.Current.Series[:min(len(cmpA.Current.Series), months+1)], Dashed: false, Color: "accent"},
		{Name: sc.Name, Points: cmpA.Scenario.Series[:min(len(cmpA.Scenario.Series), months+1)], Dashed: true, Color: "accent"},
	}
	if hasB {
		series = append(series, chartSeries{Name: scB.Name, Points: cmpB.Scenario.Series[:min(len(cmpB.Scenario.Series), months+1)], Dashed: true, Color: "partner-b"})
	}

	return compareView{
		Scenario: sc, ScenarioB: scB, OtherScens: others, SelectedB: selectedB, Currency: cur,
		Rows: rows, GoalRows: goalRows, TransferRows: transferRows,
		Payload: projPayload{Series: series, Currency: cur, HorizonYears: service.ProjectionYears[len(service.ProjectionYears)-1]},
	}
}

func moneyRow(label string, curV, aV int64, cmpB *service.Comparison, hasB bool, currency string, pick func(service.Snapshot) int64) metricRow {
	aText, aTone := formatSignedMoney(aV-curV, currency)
	row := metricRow{Label: label, Current: service.Money(curV, currency), ScenA: service.Money(aV, currency), DeltaA: aText, DeltaATone: aTone}
	if hasB {
		bV := pick(cmpB.Scenario)
		bText, bTone := formatSignedMoney(bV-curV, currency)
		row.ScenB, row.DeltaB, row.DeltaBTone, row.HasB = service.Money(bV, currency), bText, bTone, true
	}
	return row
}

func ratioRow(label string, cur, a service.Ratio, cmpB *service.Comparison, hasB bool) metricRow {
	row := metricRow{
		Label: label, Current: view.Pct(cur.A) + " / " + view.Pct(cur.B),
		ScenA: view.Pct(a.A) + " / " + view.Pct(a.B), DeltaATone: "neutral",
		DeltaA: fmt.Sprintf("%+.1f pp", (a.A-cur.A)*100),
	}
	if hasB {
		b := cmpB.Scenario.Overview.Ratio
		row.ScenB = view.Pct(b.A) + " / " + view.Pct(b.B)
		row.DeltaB = fmt.Sprintf("%+.1f pp", (b.A-cur.A)*100)
		row.DeltaBTone = "neutral"
		row.HasB = true
	}
	return row
}

func metaEtaRow(name string, cur, a service.GoalProgress) metricRow {
	curText := etaText(cur)
	aText, aDelta, aTone := etaCell(cur, a)
	return metricRow{Label: name, Current: curText, ScenA: aText, DeltaA: aDelta, DeltaATone: aTone}
}

func etaText(g service.GoalProgress) string {
	switch {
	case g.ReachedAlready:
		return "reached"
	case g.ETAMonths < 0:
		return "unreachable"
	default:
		return fmt.Sprintf("%d mo", g.ETAMonths)
	}
}

// etaCell compares scenario ETA `s` against current ETA `cur`, returning the
// scenario's own text plus a signed delta description. Sooner is favorable.
func etaCell(cur, s service.GoalProgress) (text, delta, tone string) {
	text = etaText(s)
	switch {
	case cur.ETAMonths >= 0 && s.ETAMonths >= 0:
		d := s.ETAMonths - cur.ETAMonths
		if d == 0 {
			return text, "±0 mo", "neutral"
		}
		sign := "+"
		if d < 0 {
			sign = ""
		}
		tone = "danger"
		if d < 0 {
			tone = "positive"
		}
		return text, fmt.Sprintf("%s%d mo", sign, d), tone
	case cur.ETAMonths >= 0 && s.ETAMonths < 0:
		return text, "now unreachable", "danger"
	case cur.ETAMonths < 0 && s.ETAMonths >= 0:
		return text, "now reachable", "positive"
	default:
		return text, "still unreachable", "neutral"
	}
}

// formatSignedMoney renders a delta with an explicit sign and a tone token
// ("positive"/"danger"/"neutral") for pill styling. Positive delta = more
// money = favorable, matching the prompt's "favorable" definition.
func formatSignedMoney(delta int64, currency string) (text, tone string) {
	if delta == 0 {
		return "±" + service.Money(0, currency), "neutral"
	}
	abs := delta
	tone = "positive"
	sign := "+"
	if delta < 0 {
		abs, sign, tone = -delta, "-", "danger"
	}
	return sign + service.Money(abs, currency), tone
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
