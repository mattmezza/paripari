package handler

import (
	"html/template"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// registerProjections mounts the projections section: the net-worth/savings
// growth chart, horizon + return-rate controls, goal markers, and scenario
// overlays.
func registerProjections(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /projections", func(w http.ResponseWriter, r *http.Request) {
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
		pd := projectionsData(d, in, r)
		pd.Scenarios = scenarios
		pd.Saved, err = savedProjections(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "projections", &view.PageData{
			Title: "Projections", Description: "Savings and net worth projections.", Data: pd,
		})
	})

	// GET /projections/data re-renders just the chart JSON blob + summary strip.
	// htmx-only endpoint: horizon pills / return-rate slider / scenario overlay
	// checkboxes all hx-get here and swap #projection-data.
	mux.HandleFunc("GET /projections/data", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// After-Swap, not plain HX-Trigger: htmx fires HX-Trigger the moment the
		// response headers are read, before the swap replaces #projection-json —
		// so the chart's refresh() would read the *previous* payload. Firing
		// after the swap means the JSON script tag is already up to date.
		w.Header().Set("HX-Trigger-After-Swap", "pp:projection-updated")
		d.View.Partial(w, "partials/projections-chart-data", projectionsData(d, in, r))
	})

	// Save / rename+restate / delete a named set of assumptions. The knobs are
	// not re-entered: both writing routes hx-include the projection form, so
	// what gets stored is the state the user is looking at.
	mux.HandleFunc("POST /projections/saved", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		p, formErr := parseSavedProjectionForm(r, sess.Household.ID)
		if formErr != "" {
			htmxFieldError(w, "#saved-projection-error", formErr)
			return
		}
		if _, err := d.Store.CreateSavedProjection(&p); err != nil {
			http.Error(w, "could not save projection", http.StatusInternalServerError)
			return
		}
		renderSavedProjections(d, w, r)
	})

	mux.HandleFunc("PUT /projections/saved/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		p, formErr := parseSavedProjectionForm(r, sess.Household.ID)
		if formErr != "" {
			htmxFieldError(w, "#saved-projection-error", formErr)
			return
		}
		p.ID, _ = strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := d.Store.UpdateSavedProjection(&p); err != nil {
			http.Error(w, "could not save projection", http.StatusInternalServerError)
			return
		}
		renderSavedProjections(d, w, r)
	})

	mux.HandleFunc("DELETE /projections/saved/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := d.Store.DeleteSavedProjection(sess.Household.ID, id); err != nil {
			http.Error(w, "could not delete projection", http.StatusInternalServerError)
			return
		}
		renderSavedProjections(d, w, r)
	})
}

// savedProjectionsData is the Data payload for the saved-projections region.
type savedProjectionsData struct {
	Saved []savedProjectionCard
	CSRF  string
}

// savedProjectionCard is a stored row plus its assumptions decoded into the
// one-line summary the list shows — the template never sees a query string.
type savedProjectionCard struct {
	model.SavedProjection
	Summary string
	// LoadURL is typed so html/template leaves the query string's & and =
	// alone; a plain string in an href's query part comes out percent-escaped.
	LoadURL template.URL
}

func savedProjections(d *Deps, r *http.Request) (*savedProjectionsData, error) {
	sess := auth.FromContext(r)
	rows, err := d.Store.SavedProjections(sess.Household.ID)
	if err != nil {
		return nil, err
	}
	sd := &savedProjectionsData{CSRF: sess.Token}
	for _, p := range rows {
		sd.Saved = append(sd.Saved, savedProjectionCard{
			SavedProjection: p,
			Summary:         summarizeParams(p.Params),
			LoadURL:         template.URL("/projections?" + p.Params),
		})
	}
	return sd, nil
}

func renderSavedProjections(d *Deps, w http.ResponseWriter, r *http.Request) {
	sd, err := savedProjections(d, r)
	if err != nil {
		http.Error(w, "could not load saved projections", http.StatusInternalServerError)
		return
	}
	d.View.Partial(w, "partials/projections-saved", sd)
}

// maxSavedParams caps the stored query string. The form that produces it is an
// order of magnitude smaller; the cap only stops a hand-crafted POST from
// parking an unbounded blob in the row.
const maxSavedParams = 4096

func parseSavedProjectionForm(r *http.Request, householdID int64) (model.SavedProjection, string) {
	r.ParseForm()
	p := model.SavedProjection{HouseholdID: householdID, Name: strings.TrimSpace(r.PostFormValue("name"))}
	if p.Name == "" {
		return p, "Name is required."
	}
	// ponytail: every posted field except the form's own two becomes a param,
	// rather than a whitelist of knob names that would need editing whenever a
	// knob is added. Ceiling: a hand-crafted POST can store fields the page
	// ignores on load; the length cap is what keeps that bounded.
	q := url.Values{}
	for k, vs := range r.PostForm {
		if k == "name" || k == "csrf_token" {
			continue
		}
		q[k] = vs
	}
	p.Params = q.Encode()
	if len(p.Params) > maxSavedParams {
		return p, "That is more settings than a saved projection can hold."
	}
	return p, ""
}

// summarizeParams decodes a stored query string into "5.0% · 10 years · …", so
// two saved projections can be told apart without loading them.
func summarizeParams(params string) string {
	q, _ := url.ParseQuery(params) // a summary is not a trust boundary: junk just summarises as defaults
	var parts []string
	if f, err := strconv.ParseFloat(q.Get("rate"), 64); err == nil {
		parts = append(parts, view.Pct(f))
	}
	parts = append(parts, count(atoiDefault(q.Get("horizon"), 10), "year"))
	if f, err := strconv.ParseFloat(q.Get("inflation"), 64); err == nil && f > 0 {
		parts = append(parts, view.Pct(f)+" inflation")
	}
	if n := len(q["oneoff_at"]); n > 0 {
		parts = append(parts, count(n, "one-off"))
	}
	// Growth only earns a mention when it is not the assumption everyone gets.
	for _, who := range []string{"a", "b"} {
		if f, err := strconv.ParseFloat(q.Get("growth_"+who), 64); err == nil && f != defaultIncomeGrowth {
			parts = append(parts, view.Pct(f)+" income growth")
		}
	}
	if n := len(q["promo_at"]); n > 0 {
		parts = append(parts, count(n, "raise"))
	}
	if q.Get("goals") == "1" {
		parts = append(parts, "spends goals")
	}
	if n := len(q["scenario"]); n > 0 {
		parts = append(parts, count(n, "overlay"))
	}
	if n := len(q["exclude"]); n > 0 {
		parts = append(parts, count(n, "exclusion"))
	}
	return strings.Join(parts, " · ")
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// projectionsViewData is the Data payload for the projections page and its
// chart-data partial.
type projectionsViewData struct {
	HorizonYears int
	Rate         float64
	RatePct      int // slider echo, 0-8 in .5 steps
	Currency     string

	StartCents          int64
	LiquidCents         int64 // accounts of every purpose, part of StartCents
	GoldCents           int64 // part of StartCents
	PropertyCents       int64 // part of StartCents
	SurplusCents        int64
	ValueAtHorizonCents int64
	ContributedCents    int64
	GrowthCents         int64

	GoldRate       float64     // gold's own assumed annual return
	StartItems     []startItem // the pickable pieces of the starting balance
	CashCents      int64
	CashStr        string
	Inflation      float64
	SpendGoals     bool
	GoalSpendCents int64 // goal targets that land inside the horizon
	OneOffs        []oneOff
	OneOffCents    int64
	// PartnerBName is empty in a solo household, which is what the template
	// checks before offering a second set of income controls.
	PartnerAName, PartnerBName string
	GrowthA, GrowthB           float64
	Promos                     []promo
	SpentCents                 int64 // OneOffCents + GoalSpendCents, for the summary line

	Goals []service.GoalProgress

	Scenarios   []model.Scenario
	SelectedIDs map[int64]bool
	Payload     projPayload

	// Saved is nil on the chart-data endpoint: that swap replaces the chart, not
	// the saved-projections region.
	Saved *savedProjectionsData
}

// startItem is one line of the starting balance the user can untick. Token is
// what travels in the `exclude` query param.
type startItem struct {
	Token    string
	Name     string
	Cents    int64
	Included bool
}

// oneOff is a dated lump-sum expense. At is a month index from today.
type oneOff struct {
	At        int
	Cents     int64
	AmountStr string
}

// promo is a timed raise for one partner: at month At their pay steps up by
// Pct. Percent is the same figure in percent units, which is what the row's
// number input shows — the query string carries the fraction, like every other
// rate knob.
type promo struct {
	At      int
	Pct     float64
	Percent float64
	Who     string // "a" or "b"
}

// defaultIncomeGrowth is the assumed annual raise when the query string is
// silent: roughly a cost-of-living adjustment.
const defaultIncomeGrowth = 0.02

// projectionKnobs is everything the query string says about the projection
// beyond horizon and rate.
type projectionKnobs struct {
	exclude    map[string]bool
	cashCents  int64
	inflation  float64
	spendGoals bool
	oneOffs    []oneOff
	growth     map[string]float64 // "a"/"b" → assumed annual income growth
	promos     []promo
}

func parseProjectionKnobs(r *http.Request) projectionKnobs {
	q := r.URL.Query()
	k := projectionKnobs{exclude: map[string]bool{}, spendGoals: q.Get("goals") == "1"}
	for _, t := range q["exclude"] {
		k.exclude[t] = true
	}
	k.cashCents, _ = ParseAmount(q.Get("cash")) // empty or junk → 0, which is the default anyway
	if f, err := strconv.ParseFloat(q.Get("inflation"), 64); err == nil && f >= 0 && f <= 0.1 {
		k.inflation = f
	}
	// Two parallel repeated params, paired by position — the rows are emitted
	// together by the form, so index i of one always matches index i of the other.
	ats, amts := q["oneoff_at"], q["oneoff_amount"]
	for i, at := range ats {
		if i >= len(amts) {
			break
		}
		months := atoiDefault(at, 0)
		cents, err := ParseAmount(amts[i])
		if err != nil || months < 1 || cents <= 0 {
			continue
		}
		k.oneOffs = append(k.oneOffs, oneOff{At: months, Cents: cents, AmountStr: CentsToStr(cents)})
	}
	// A missing growth knob means the default, not zero: a projection saved
	// before this knob existed still assumes the usual cost-of-living raise.
	k.growth = map[string]float64{"a": defaultIncomeGrowth, "b": defaultIncomeGrowth}
	for who := range k.growth {
		// Negative on purpose: a pay cut is a scenario worth being able to draw.
		if f, err := strconv.ParseFloat(q.Get("growth_"+who), 64); err == nil && f >= -0.10 && f <= 0.10 {
			k.growth[who] = f
		}
	}
	// Same positional pairing as the one-offs, one array wider: which partner.
	pats, ppcts, pwhos := q["promo_at"], q["promo_pct"], q["promo_who"]
	for i, at := range pats {
		if i >= len(ppcts) || i >= len(pwhos) {
			break
		}
		months := atoiDefault(at, 0)
		pct, err := strconv.ParseFloat(ppcts[i], 64)
		who := pwhos[i]
		if err != nil || months < 1 || pct < -1 || pct > 2 || (who != "a" && who != "b") {
			continue
		}
		// Rounded because 0.08*100 is not 8 in binary floating point, and this
		// number is typed straight back into the row's input.
		k.promos = append(k.promos, promo{At: months, Pct: pct, Percent: math.Round(pct*10000) / 100, Who: who})
	}
	return k
}

// incomeStreams turns each partner's net income into an engine growth stream.
// A solo household has a zero-value partner B: it gets no stream, and the
// promotion rows aimed at it were already dropped in projectionsData.
func incomeStreams(ov service.MonthlyOverview, k projectionKnobs) []service.IncomeStream {
	var out []service.IncomeStream
	for _, p := range []struct {
		who string
		cf  service.PartnerCashflow
	}{{"a", ov.A}, {"b", ov.B}} {
		if p.cf.UserID == 0 {
			continue
		}
		s := service.IncomeStream{MonthlyNetCents: p.cf.NetIncomeCents, AnnualGrowth: k.growth[p.who]}
		for _, pr := range k.promos {
			if pr.Who == p.who {
				s.Promotions = append(s.Promotions, service.Promotion{AtMonth: pr.At, Pct: pr.Pct})
			}
		}
		out = append(out, s)
	}
	return out
}

// applyExclusions drops the unticked pieces of the starting balance and reports
// what was on offer. Excluding an account also takes it out of the goal pot —
// money you told us to ignore can't be funding a goal either.
func applyExclusions(in service.Inputs, ex map[string]bool) (service.Inputs, []startItem) {
	var items []startItem
	accounts := in.Accounts[:0:0]
	for _, a := range in.Accounts {
		tok := "a" + strconv.FormatInt(a.ID, 10)
		items = append(items, startItem{Token: tok, Name: a.Name, Cents: convertRate(in.Rates, a.BalanceCents, a.Currency, in.Display), Included: !ex[tok]})
		if !ex[tok] {
			accounts = append(accounts, a)
		}
	}
	nw := service.ComputeNetWorth(in)
	if nw.AlternativeCents > 0 {
		items = append(items, startItem{Token: "gold", Name: "Gold", Cents: nw.AlternativeCents, Included: !ex["gold"]})
	}
	if nw.RealEstateCents > 0 {
		items = append(items, startItem{Token: "property", Name: "Property", Cents: nw.RealEstateCents, Included: !ex["property"]})
	}
	in.Accounts = accounts
	if ex["gold"] {
		in.Gold = nil
	}
	if ex["property"] {
		in.Assets = nil
	}
	return in, items
}

func projectionsData(d *Deps, in service.Inputs, r *http.Request) *projectionsViewData {
	horizon := atoiDefault(r.URL.Query().Get("horizon"), 10)
	if horizon != 1 && horizon != 5 && horizon != 10 && horizon != 20 {
		horizon = 10
	}
	rate := in.AnnualReturnRate
	if v := r.URL.Query().Get("rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= -0.10 && f <= 0.10 {
			rate = f
		}
	}
	in.AnnualReturnRate = rate
	goldRate := 0.07
	if v := r.URL.Query().Get("gold_rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 0.15 {
			goldRate = f
		}
	}

	k := parseProjectionKnobs(r)
	in, startItems := applyExclusions(in, k.exclude)

	nw := service.ComputeNetWorth(in)
	ov := service.BuildOverview(in)
	goals := service.GoalProgresses(in, ov)

	if ov.B.UserID == 0 {
		k.promos = slices.DeleteFunc(k.promos, func(p promo) bool { return p.Who == "b" })
	}

	months := horizon * 12
	events := map[int]int64{}
	for _, o := range k.oneOffs {
		if o.At <= months {
			events[o.At] += o.Cents
		}
	}
	var oneOffTotal int64
	for _, v := range events {
		oneOffTotal += v
	}
	// ponytail: goals are spent at the ETA they'd reach *without* this spending
	// — no fixed-point iteration. Two goals racing for the same money will both
	// land at their solo ETA, which is optimistic; fine for a what-if.
	var goalSpend int64
	if k.spendGoals {
		for _, g := range goals {
			at := max(g.ETAMonths, 1)
			if g.ETAMonths < 0 || at > months {
				continue
			}
			cents := convertRate(in.Rates, g.TargetCents, g.Goal.Currency, in.Display)
			events[at] += cents
			goalSpend += cents
		}
	}

	spec := service.ProjectionSpec{
		StartCents: nw.TotalCents, CashCents: k.cashCents, MonthlyCents: ov.SurplusCents,
		// nw is computed after applyExclusions, so AlternativeCents is already 0
		// when gold is unticked (in.Gold is nil'd there).
		GoldCents:    nw.AlternativeCents,
		AnnualReturn: rate, GoldAnnualReturn: goldRate, AnnualInflation: k.inflation, Events: events, Months: months,
		Incomes: incomeStreams(ov, k),
	}
	proj := service.Project(spec)
	pts := proj.Points
	last := pts[len(pts)-1]

	pd := &projectionsViewData{
		HorizonYears: horizon, Rate: rate, RatePct: int(rate*200 + 0.5), Currency: in.Display,
		StartCents: nw.TotalCents, SurplusCents: ov.SurplusCents,
		LiquidCents: nw.LiquidCents, GoldCents: nw.AlternativeCents, PropertyCents: nw.RealEstateCents,
		ValueAtHorizonCents: last.BalanceCents, ContributedCents: proj.ContributedCents, GrowthCents: proj.GrowthCents,
		GoldRate:   goldRate,
		StartItems: startItems, CashCents: k.cashCents, CashStr: CentsToStr(k.cashCents),
		Inflation: k.inflation, SpendGoals: k.spendGoals, GoalSpendCents: goalSpend,
		OneOffs: k.oneOffs, OneOffCents: oneOffTotal, SpentCents: oneOffTotal + goalSpend,
		PartnerAName: ov.A.Name, PartnerBName: ov.B.Name,
		GrowthA: k.growth["a"], GrowthB: k.growth["b"], Promos: k.promos,
		Goals: goals,
	}

	series := []chartSeries{{Name: "Current", Points: pts, Dashed: false, Color: "accent"}}
	var goalMarkers []goalMarker
	for _, g := range goals {
		vc := convertRate(in.Rates, g.Goal.TargetAmountCents, g.Goal.Currency, in.Display)
		goalMarkers = append(goalMarkers, goalMarker{Name: g.Goal.Name, ValueCents: vc})
	}

	scenarioIDs := r.URL.Query()["scenario"]
	selected := map[int64]bool{}
	colorTokens := []string{"partner-b", "positive", "warning"}
	if len(scenarioIDs) > 0 {
		all, err := d.Store.Scenarios(in.Household.ID)
		if err == nil {
			byID := map[int64]int{}
			for i, sc := range all {
				byID[sc.ID] = i
			}
			ci := 0
			for _, idStr := range scenarioIDs {
				id, err := strconv.ParseInt(idStr, 10, 64)
				if err != nil {
					continue
				}
				i, ok := byID[id]
				if !ok {
					continue
				}
				selected[id] = true
				sc := all[i]
				scIn := service.Apply(in, sc.Changes)
				scNW := service.ComputeNetWorth(scIn)
				scOv := service.BuildOverview(scIn)
				scSpec := spec
				scSpec.StartCents, scSpec.MonthlyCents = scNW.TotalCents, scOv.SurplusCents
				scSpec.AnnualReturn = scIn.AnnualReturnRate
				scSpec.GoldCents = scNW.AlternativeCents // gold rate carries over from the slider
				// The raises are assumptions about the people, not about this
				// scenario, so they apply to whatever income the scenario leaves them.
				scSpec.Incomes = incomeStreams(scOv, k)
				scPts := service.Project(scSpec).Points
				token := colorTokens[ci%len(colorTokens)]
				ci++
				series = append(series, chartSeries{Name: sc.Name, Points: scPts, Dashed: true, Color: token})
			}
		}
	}
	pd.SelectedIDs = selected
	pd.Payload = projPayload{Series: series, Goals: goalMarkers, Currency: in.Display, HorizonYears: horizon}
	return pd
}

// convertRate converts using a service.Rates provider, tolerating a nil rates
// (identity) the same way the engine's unexported conv() does.
func convertRate(r service.Rates, cents int64, from, to string) int64 {
	if r == nil || from == to || from == "" || to == "" {
		return cents
	}
	return r.Convert(cents, from, to)
}

type chartSeries struct {
	Name   string          `json:"name"`
	Points []service.Point `json:"points"`
	Dashed bool            `json:"dashed"`
	Color  string          `json:"color"`
}

type goalMarker struct {
	Name       string `json:"name"`
	ValueCents int64  `json:"value"`
}

type projPayload struct {
	Series       []chartSeries `json:"series"`
	Goals        []goalMarker  `json:"goals"`
	Currency     string        `json:"currency"`
	HorizonYears int           `json:"horizon"`
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
