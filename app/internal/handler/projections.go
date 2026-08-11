package handler

import (
	"net/http"
	"strconv"

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
		w.Header().Set("HX-Trigger", "pp:projection-updated")
		d.View.Partial(w, "partials/projections-chart-data", projectionsData(d, in, r))
	})
}

// projectionsViewData is the Data payload for the projections page and its
// chart-data partial.
type projectionsViewData struct {
	HorizonYears int
	Rate         float64
	RatePct      int // slider echo, 0-8 in .5 steps
	Currency     string

	StartCents          int64
	SurplusCents        int64
	ValueAtHorizonCents int64
	ContributedCents    int64
	GrowthCents         int64

	Goals []service.GoalProgress

	Scenarios   []model.Scenario
	SelectedIDs map[int64]bool
	Payload     projPayload
}

func projectionsData(d *Deps, in service.Inputs, r *http.Request) *projectionsViewData {
	horizon := atoiDefault(r.URL.Query().Get("horizon"), 10)
	if horizon != 1 && horizon != 5 && horizon != 10 && horizon != 20 {
		horizon = 10
	}
	rate := in.AnnualReturnRate
	if v := r.URL.Query().Get("rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 0.08 {
			rate = f
		}
	}
	in.AnnualReturnRate = rate

	nw := service.ComputeNetWorth(in)
	ov := service.BuildOverview(in)
	goals := service.GoalProgresses(in, ov)

	months := horizon * 12
	pts := service.ProjectSavings(nw.TotalCents, ov.SurplusCents, rate, months)
	last := pts[len(pts)-1]

	contributed := ov.SurplusCents * int64(months)
	growth := last.BalanceCents - nw.TotalCents - contributed

	pd := &projectionsViewData{
		HorizonYears: horizon, Rate: rate, RatePct: int(rate*200 + 0.5), Currency: in.Display,
		StartCents: nw.TotalCents, SurplusCents: ov.SurplusCents,
		ValueAtHorizonCents: last.BalanceCents, ContributedCents: contributed, GrowthCents: growth,
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
				scPts := service.ProjectSavings(scNW.TotalCents, scOv.SurplusCents, scIn.AnnualReturnRate, months)
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
