package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// tripLinkedSubcategory is how a committed trip's funding expense is tied back
// to the trip: the expense's subcategory encodes the trip id, so uncommitting
// can find and delete it without a schema change (trip_plans has no
// linked_expense_id column).
// ponytail: a real FK column would be cleaner; this piggybacks on the existing
// subcategory text field to avoid touching migrations owned by another agent.
func tripLinkedSubcategory(tripID int64) string { return fmt.Sprintf("trip:%d", tripID) }

// tripCategories are the itemized cost buckets from prompt.md's trip planner.
var tripCategories = []string{"flights", "accommodation", "activities", "meals", "transport", "other"}

// registerTrips mounts the holiday/trip budgeting section: itemized plans,
// funding math, goal-impact comparison, and commit-to-expense.
func registerTrips(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /trips", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		trips, err := d.Store.Trips(in.Household.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "trips", &view.PageData{
			Title: "Trips", Description: "Plan and fund your holidays.",
			Data: tripListData(trips, in),
		})
	})

	mux.HandleFunc("POST /trips", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		name := r.PostFormValue("name")
		if name == "" {
			name = "Untitled trip"
		}
		t := &model.TripPlan{HouseholdID: in.Household.ID, Name: name, MonthsToSave: 1}
		if sd := r.PostFormValue("start_date"); sd != "" {
			t.StartDate = &sd
		}
		id, err := d.Store.CreateTrip(t)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirect(w, r, fmt.Sprintf("/trips/%d", id))
	})

	mux.HandleFunc("GET /trips/{id}", func(w http.ResponseWriter, r *http.Request) {
		in, t, err := loadTrip(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		d.View.Render(w, r, "trip-detail", &view.PageData{
			Title: t.Name, Description: "Trip budget and impact.", Data: tripDetailData(in, *t),
		})
	})

	mux.HandleFunc("POST /trips/{id}", func(w http.ResponseWriter, r *http.Request) {
		in, t, err := loadTrip(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		t.Name = r.PostFormValue("name")
		if t.Name == "" {
			t.Name = "Untitled trip"
		}
		if sd := r.PostFormValue("start_date"); sd != "" {
			t.StartDate = &sd
		} else {
			t.StartDate = nil
		}
		months, err := strconv.Atoi(r.PostFormValue("months_to_save"))
		if err != nil || months < 1 {
			months = 1
		}
		t.MonthsToSave = months
		if err := d.Store.UpdateTrip(t); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t2, err := d.Store.Trip(in.Household.ID, t.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/trips-detail", tripDetailData(in, *t2))
	})

	mux.HandleFunc("DELETE /trips/{id}", func(w http.ResponseWriter, r *http.Request) {
		in, t, err := loadTrip(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if t.Committed {
			removeLinkedExpense(d, in, t.ID)
		}
		if err := d.Store.DeleteTrip(in.Household.ID, t.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirect(w, r, "/trips")
	})

	mux.HandleFunc("POST /trips/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		in, t, err := loadTrip(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		it, err := parseTripItemForm(r, t.ID, in.Display)
		if err != nil {
			data := tripDetailData(in, *t)
			data.FormError = err.Error()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			d.View.Partial(w, "partials/trips-detail", data)
			return
		}
		if _, err := d.Store.CreateTripItem(in.Household.ID, &it); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t2, err := d.Store.Trip(in.Household.ID, t.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/trips-detail", tripDetailData(in, *t2))
	})

	mux.HandleFunc("POST /trips/{id}/items/{itemID}", func(w http.ResponseWriter, r *http.Request) {
		in, t, err := loadTrip(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		itemID, _ := strconv.ParseInt(r.PathValue("itemID"), 10, 64)
		it, err := parseTripItemForm(r, t.ID, in.Display)
		if err != nil {
			data := tripDetailData(in, *t)
			data.FormError = err.Error()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			d.View.Partial(w, "partials/trips-detail", data)
			return
		}
		it.ID = itemID
		if err := d.Store.UpdateTripItem(in.Household.ID, &it); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t2, err := d.Store.Trip(in.Household.ID, t.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/trips-detail", tripDetailData(in, *t2))
	})

	mux.HandleFunc("DELETE /trips/{id}/items/{itemID}", func(w http.ResponseWriter, r *http.Request) {
		in, t, err := loadTrip(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		itemID, _ := strconv.ParseInt(r.PathValue("itemID"), 10, 64)
		if err := d.Store.DeleteTripItem(in.Household.ID, itemID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t2, err := d.Store.Trip(in.Household.ID, t.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/trips-detail", tripDetailData(in, *t2))
	})

	mux.HandleFunc("POST /trips/{id}/commit", func(w http.ResponseWriter, r *http.Request) {
		in, t, err := loadTrip(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		tt := service.TripTotal(*t, in.Rates, in.Display)
		e := model.Expense{
			HouseholdID: in.Household.ID, Name: "Trip: " + t.Name,
			AmountCents: tt.MonthlyCents, Currency: tt.Currency,
			Category: "common", Subcategory: tripLinkedSubcategory(t.ID), Kind: model.KindExpense,
		}
		if _, err := d.Store.CreateExpense(&e); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t.Committed = true
		if err := d.Store.UpdateTrip(t); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		view.SetFlash(w, "Trip committed. "+service.Money(tt.MonthlyCents, tt.Currency)+"/mo added to common expenses.")
		t2, _ := d.Store.Trip(in.Household.ID, t.ID)
		in2, _ := BuildInputs(d, r)
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/trips-detail", tripDetailData(in2, *t2))
	})

	mux.HandleFunc("POST /trips/{id}/uncommit", func(w http.ResponseWriter, r *http.Request) {
		in, t, err := loadTrip(d, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		removeLinkedExpense(d, in, t.ID)
		t.Committed = false
		if err := d.Store.UpdateTrip(t); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		view.SetFlash(w, "Trip uncommitted. The funding expense was removed.")
		t2, _ := d.Store.Trip(in.Household.ID, t.ID)
		in2, _ := BuildInputs(d, r)
		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/trips-detail", tripDetailData(in2, *t2))
	})
}

func loadTrip(d *Deps, r *http.Request) (service.Inputs, *model.TripPlan, error) {
	in, err := BuildInputs(d, r)
	if err != nil {
		return in, nil, err
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return in, nil, err
	}
	t, err := d.Store.Trip(in.Household.ID, id)
	if err != nil {
		return in, nil, err
	}
	return in, t, nil
}

// removeLinkedExpense deletes the common expense a commit created for this
// trip, if any (see tripLinkedSubcategory).
func removeLinkedExpense(d *Deps, in service.Inputs, tripID int64) {
	want := tripLinkedSubcategory(tripID)
	for _, e := range in.Expenses {
		if e.Subcategory == want {
			d.Store.DeleteExpense(in.Household.ID, e.ID)
		}
	}
}

func parseTripItemForm(r *http.Request, tripID int64, defaultCurrency string) (model.TripItem, error) {
	name := r.PostFormValue("name")
	if name == "" {
		return model.TripItem{}, fmt.Errorf("name is required")
	}
	cat := r.PostFormValue("category")
	cents, err := ParseAmount(r.PostFormValue("amount"))
	if err != nil {
		return model.TripItem{}, err
	}
	if cents < 0 {
		return model.TripItem{}, fmt.Errorf("amount can't be negative")
	}
	cur := orDefault2(r.PostFormValue("currency"), defaultCurrency)
	return model.TripItem{TripPlanID: tripID, Name: name, Category: cat, AmountCents: cents, Currency: cur}, nil
}

// ─── List page ──────────────────────────────────────────────────────────────

type tripCard struct {
	model.TripPlan
	TotalText     string
	Status        string // draft | committed
	StartDateText string
}

type tripListView struct {
	Trips    []tripCard
	Currency string
}

func tripListData(trips []model.TripPlan, in service.Inputs) tripListView {
	cards := make([]tripCard, 0, len(trips))
	for _, t := range trips {
		tt := service.TripTotal(t, in.Rates, in.Display)
		status := "draft"
		if t.Committed {
			status = "committed"
		}
		sd := ""
		if t.StartDate != nil {
			sd = view.Date(*t.StartDate)
		}
		cards = append(cards, tripCard{TripPlan: t, TotalText: service.Money(tt.TotalCents, tt.Currency), Status: status, StartDateText: sd})
	}
	return tripListView{Trips: cards, Currency: in.Display}
}

// ─── Detail page ────────────────────────────────────────────────────────────

type tripDetailView struct {
	Trip           model.TripPlan
	StartDateValue string
	Currency       string
	Currencies     []string
	Categories     []string
	FormError      string

	Totals      service.TripTotals
	TotalText   string
	MonthlyText string
	ByCategory  []categoryTotal

	AvailableDeltaText string
	AvailableDeltaTone string
	GoalRows           []metricRow
	Payload            projPayload

	FundedSoFarCents int64
	FundedRatio      float64 // 0..1
}

type categoryTotal struct {
	Category string
	Text     string
}

func tripDetailData(in service.Inputs, t model.TripPlan) *tripDetailView {
	tt, cmp := service.TripImpact(in, t)

	byCat := make([]categoryTotal, 0, len(tripCategories))
	for _, c := range tripCategories {
		if v, ok := tt.ByCategory[c]; ok {
			byCat = append(byCat, categoryTotal{Category: c, Text: service.Money(v, tt.Currency)})
		}
	}

	availDelta := cmp.Scenario.Overview.AvailableCents - cmp.Current.Overview.AvailableCents
	availText, availTone := formatSignedMoney(availDelta, in.Display)

	var goalRows []metricRow
	aETA := map[int64]service.GoalProgress{}
	for _, g := range cmp.Scenario.Goals {
		aETA[g.Goal.ID] = g
	}
	for _, g := range cmp.Current.Goals {
		goalRows = append(goalRows, metaEtaRow(g.Goal.Name, g, aETA[g.Goal.ID]))
	}

	months := service.ProjectionYears[len(service.ProjectionYears)-1] * 12
	series := []chartSeries{
		{Name: "Without this trip", Points: cmp.Current.Series[:min(len(cmp.Current.Series), months+1)], Dashed: false, Color: "accent"},
		{Name: "With this trip", Points: cmp.Scenario.Series[:min(len(cmp.Scenario.Series), months+1)], Dashed: true, Color: "partner-b"},
	}

	var fundedSoFar int64
	var ratio float64
	if t.Committed {
		// ponytail: trip_plans has no committed_at column, so CreatedAt is the
		// best available proxy for "when funding started". Add a real
		// committed_at timestamp if precise progress tracking matters later.
		if created, err := time.Parse(time.RFC3339, t.CreatedAt); err == nil {
			elapsed := int(time.Since(created).Hours() / (24 * 30))
			if elapsed < 0 {
				elapsed = 0
			}
			fundedSoFar = int64(elapsed) * tt.MonthlyCents
			if fundedSoFar > tt.TotalCents {
				fundedSoFar = tt.TotalCents
			}
			if tt.TotalCents > 0 {
				ratio = float64(fundedSoFar) / float64(tt.TotalCents)
				if ratio > 1 {
					ratio = 1
				}
			}
		}
	}

	sdv := ""
	if t.StartDate != nil {
		sdv = *t.StartDate
	}

	return &tripDetailView{
		Trip: t, StartDateValue: sdv, Currency: in.Display, Currencies: view.Currencies, Categories: tripCategories,
		Totals: tt, TotalText: service.Money(tt.TotalCents, tt.Currency), MonthlyText: service.Money(tt.MonthlyCents, tt.Currency),
		ByCategory:         byCat,
		AvailableDeltaText: availText, AvailableDeltaTone: availTone,
		GoalRows:         goalRows,
		Payload:          projPayload{Series: series, Currency: in.Display, HorizonYears: service.ProjectionYears[len(service.ProjectionYears)-1]},
		FundedSoFarCents: fundedSoFar, FundedRatio: ratio,
	}
}
