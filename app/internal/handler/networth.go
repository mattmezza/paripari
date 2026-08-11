package handler

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/jobs"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// nwAssetRow pairs a stored asset with its value converted into the page's
// display currency, so the template never converts money itself.
type nwAssetRow struct {
	model.Asset
	ValueCents int64
}

// nwPoint is one point on the historical trend line, in the page's display
// currency. JSON tags match Chart.js's x/y expectations loosely (date string
// + cents); the chart script converts cents to major units.
type nwPoint struct {
	Date  string `json:"d"`
	Value int64  `json:"v"`
}

type nwBreakdown struct {
	Liquid      int64 `json:"liquid"`
	Alternative int64 `json:"alternative"`
	RealEstate  int64 `json:"realEstate"`
}

// nwPayload is what the page's inline script reads to draw both charts.
type nwPayload struct {
	Currency   string      `json:"currency"`
	Breakdown  nwBreakdown `json:"breakdown"`
	History    []nwPoint   `json:"history"`
	HasHistory bool        `json:"hasHistory"`
}

type nwData struct {
	Currency    string
	Currencies  []string
	Liquid      int64
	Alternative int64
	RealEstate  int64
	Total       int64
	ByPurpose   map[string]int64
	Assets      []nwAssetRow
	HasHistory  bool
	CSRF        string
	Payload     nwPayload
}

// centsToPlain formats cents as a plain decimal ("1234.50", no currency code,
// no thousands separator) so ParseAmount can round-trip it as an input value.
func centsToPlain(cents int64) string {
	neg := ""
	if cents < 0 {
		neg, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", neg, cents/100, cents%100)
}

type nwAssetFormData struct {
	Asset      model.Asset
	AmountStr  string // plain "1234.50", no currency prefix — pre-fills the edit input
	Currencies []string
	CSRF       string
}

// registerNetWorth mounts the net-worth section: the liquid/total breakdown,
// the breakdown + trend charts, and inline CRUD for manually-valued assets
// (real estate / other) — the only mutable data this screen owns.
func registerNetWorth(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /net-worth", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		nd, err := buildNetWorthData(d, r, sess)
		if err != nil {
			http.Error(w, "could not load net worth", http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "net-worth", &view.PageData{
			Title: "Net worth", Description: "Liquid, gold and property, all in one picture.", Data: nd,
		})
	})

	mux.HandleFunc("GET /net-worth/assets/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		a, err := d.Store.Asset(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		d.View.Partial(w, "partials/networth-asset-row-edit", &nwAssetFormData{
			Asset: *a, AmountStr: centsToPlain(a.EstimatedValueCents), Currencies: view.Currencies, CSRF: sess.Token,
		})
	})

	mux.HandleFunc("GET /net-worth/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		a, err := d.Store.Asset(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		cur := netWorthCurrency(r, sess.Household)
		d.View.Partial(w, "partials/networth-asset-row", nwAssetRow{
			Asset: *a, ValueCents: d.Rates.Convert(a.EstimatedValueCents, a.Currency, cur),
		})
	})

	mux.HandleFunc("POST /net-worth/assets", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		a, formErr := parseAssetForm(r, sess.Household.ID)
		if formErr != "" {
			htmxFieldError(w, "#nw-asset-form-error", formErr)
			return
		}
		if _, err := d.Store.CreateAsset(&a); err != nil {
			http.Error(w, "could not save asset", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(context.Background(), sess.Household.ID)
		renderNetWorthMain(d, w, r, sess)
	})

	mux.HandleFunc("PUT /net-worth/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		a, formErr := parseAssetForm(r, sess.Household.ID)
		if formErr != "" {
			htmxFieldError(w, "#nw-asset-form-error", formErr)
			return
		}
		a.ID = id
		if err := d.Store.UpdateAsset(&a); err != nil {
			http.Error(w, "could not save asset", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(context.Background(), sess.Household.ID)
		renderNetWorthMain(d, w, r, sess)
	})

	mux.HandleFunc("DELETE /net-worth/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := d.Store.DeleteAsset(sess.Household.ID, id); err != nil {
			http.Error(w, "could not delete asset", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(context.Background(), sess.Household.ID)
		renderNetWorthMain(d, w, r, sess)
	})
}

// htmxFieldError redirects an htmx swap to a small inline error slot near the
// form (HX-Retarget/HX-Reswap) instead of clobbering the whole region, so the
// entered values survive. Shared by the net-worth, gold and goals handlers —
// all three files are in package handler, defined once here.
func htmxFieldError(w http.ResponseWriter, target, msg string) {
	w.Header().Set("HX-Retarget", target)
	w.Header().Set("HX-Reswap", "innerHTML")
	w.WriteHeader(http.StatusUnprocessableEntity)
	w.Write([]byte(`<span class="helper helper--error">` + template.HTMLEscapeString(msg) + `</span>`))
}

// netWorthCurrency resolves the view-only currency switcher: a ?currency=
// query param overrides the household's stored display currency for this
// render only, nothing is persisted.
func netWorthCurrency(r *http.Request, hh *model.Household) string {
	if c := r.URL.Query().Get("currency"); c != "" && ValidCurrency(c) {
		return c
	}
	return hh.DisplayCurrency
}

func renderNetWorthMain(d *Deps, w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	nd, err := buildNetWorthData(d, r, sess)
	if err != nil {
		http.Error(w, "could not load net worth", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "pp:recalc")
	d.View.Partial(w, "partials/networth-main", nd)
}

func buildNetWorthData(d *Deps, r *http.Request, sess *auth.Session) (*nwData, error) {
	in, err := BuildInputs(d, r)
	if err != nil {
		return nil, err
	}
	cur := netWorthCurrency(r, &in.Household)
	in.Display = cur
	if cur != in.Household.DisplayCurrency && d.Gold != nil {
		if p, err := d.Gold.PricePerGramCents(in.Household.ID, cur); err == nil {
			in.GoldPricePerGramCents = p
		}
	}
	nw := service.ComputeNetWorth(in)

	assets, err := d.Store.Assets(in.Household.ID)
	if err != nil {
		return nil, err
	}
	rows := make([]nwAssetRow, 0, len(assets))
	for _, a := range assets {
		rows = append(rows, nwAssetRow{Asset: a, ValueCents: d.Rates.Convert(a.EstimatedValueCents, a.Currency, cur)})
	}

	snaps, err := d.Store.Snapshots(in.Household.ID, 180)
	if err != nil {
		return nil, err
	}
	history := make([]nwPoint, 0, len(snaps))
	for _, s := range snaps {
		total := s.LiquidCents + s.AlternativeCents + s.RealEstateCents
		history = append(history, nwPoint{Date: s.Date, Value: d.Rates.Convert(total, "CHF", cur)})
	}

	nd := &nwData{
		Currency: cur, Currencies: view.Currencies,
		Liquid: nw.LiquidCents, Alternative: nw.AlternativeCents, RealEstate: nw.RealEstateCents, Total: nw.TotalCents,
		ByPurpose: nw.ByPurpose, Assets: rows, HasHistory: len(history) > 0, CSRF: sess.Token,
		Payload: nwPayload{
			Currency:   cur,
			Breakdown:  nwBreakdown{Liquid: nw.LiquidCents, Alternative: nw.AlternativeCents, RealEstate: nw.RealEstateCents},
			History:    history,
			HasHistory: len(history) > 0,
		},
	}
	return nd, nil
}

func parseAssetForm(r *http.Request, householdID int64) (model.Asset, string) {
	r.ParseForm()
	a := model.Asset{HouseholdID: householdID}
	a.Name = r.PostFormValue("name")
	if a.Name == "" {
		return a, "Name is required."
	}
	a.Kind = r.PostFormValue("kind")
	if a.Kind != "other" {
		a.Kind = "real_estate"
	}
	a.Currency = r.PostFormValue("currency")
	if !ValidCurrency(a.Currency) {
		return a, "Choose a supported currency."
	}
	val, err := ParseAmount(r.PostFormValue("estimated_value"))
	if err != nil || val < 0 {
		return a, "Estimated value is not valid."
	}
	a.EstimatedValueCents = val
	return a, ""
}
