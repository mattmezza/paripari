package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/jobs"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// goldItemRow pairs a stored gold item with its computed value in the
// household's display currency.
type goldItemRow struct {
	model.GoldItem
	ValueCents int64
	Currency   string
}

type goldData struct {
	Currency          string
	PricePerGramCents int64
	PriceAge          string // ISO timestamp of the cached/manual price, "" if unavailable
	PriceErr          string // set when no price could be resolved at all
	Items             []goldItemRow
	TotalFineGrams    float64
	TotalItems        int
	TotalValueCents   int64
	CSRF              string
}

type goldItemFormData struct {
	Item  model.GoldItem
	CSRF  string
	Error string
}

// registerGold mounts the gold section: spot price header, inventory table
// with inline htmx add/edit/delete, and the totals footer row.
func registerGold(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /gold", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		gd, err := buildGoldData(d, sess)
		if err != nil {
			http.Error(w, "could not load gold inventory", http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "gold", &view.PageData{
			Title: "Gold", Description: "Physical gold, valued at today's spot price.", Data: gd,
		})
	})

	mux.HandleFunc("GET /gold/items/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		item, err := d.Store.GoldItem(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		d.View.Partial(w, "partials/gold-item-row-edit", &goldItemFormData{Item: *item, CSRF: sess.Token})
	})

	mux.HandleFunc("GET /gold/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		item, err := d.Store.GoldItem(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		price := goldPriceOrZero(d, sess.Household.DisplayCurrency)
		d.View.Partial(w, "partials/gold-item-row", goldItemRow{
			GoldItem: *item, Currency: sess.Household.DisplayCurrency,
			ValueCents: service.GoldValue(*item, price),
		})
	})

	mux.HandleFunc("POST /gold/items", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		item, formErr := parseGoldItemForm(r, sess.Household.ID)
		if formErr != "" {
			htmxFieldError(w, "#gold-item-form-error", formErr)
			return
		}
		if _, err := d.Store.CreateGoldItem(&item); err != nil {
			http.Error(w, "could not save gold item", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(context.Background(), sess.Household.ID)
		renderGoldMain(d, w, r, sess)
	})

	mux.HandleFunc("PUT /gold/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		item, formErr := parseGoldItemForm(r, sess.Household.ID)
		if formErr != "" {
			htmxFieldError(w, "#gold-item-form-error", formErr)
			return
		}
		item.ID = id
		if err := d.Store.UpdateGoldItem(&item); err != nil {
			http.Error(w, "could not save gold item", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(context.Background(), sess.Household.ID)
		renderGoldMain(d, w, r, sess)
	})

	mux.HandleFunc("DELETE /gold/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := d.Store.DeleteGoldItem(sess.Household.ID, id); err != nil {
			http.Error(w, "could not delete gold item", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(context.Background(), sess.Household.ID)
		renderGoldMain(d, w, r, sess)
	})
}

func renderGoldMain(d *Deps, w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	gd, err := buildGoldData(d, sess)
	if err != nil {
		http.Error(w, "could not load gold inventory", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Trigger", "pp:recalc")
	d.View.Partial(w, "partials/gold-main", gd)
}

// goldPriceOrZero resolves today's 24K gram price, tolerating "no price yet"
// (items just value at zero rather than failing the whole page).
func goldPriceOrZero(d *Deps, currency string) int64 {
	if d.Gold == nil {
		return 0
	}
	p, err := d.Gold.PricePerGramCents(currency)
	if err != nil {
		return 0
	}
	return p
}

func buildGoldData(d *Deps, sess *auth.Session) (*goldData, error) {
	cur := sess.Household.DisplayCurrency
	items, err := d.Store.GoldItems(sess.Household.ID)
	if err != nil {
		return nil, err
	}

	gd := &goldData{Currency: cur, CSRF: sess.Token}
	if d.Gold != nil {
		if p, err := d.Gold.PricePerGramCents(cur); err != nil {
			gd.PriceErr = "Couldn't reach the gold price provider. Set a manual override in settings."
		} else {
			gd.PricePerGramCents = p
		}
	}
	if gp, err := d.Store.LatestGoldPrice(); err == nil {
		gd.PriceAge = gp.FetchedAt
	}

	rows := make([]goldItemRow, 0, len(items))
	for _, it := range items {
		v := service.GoldValue(it, gd.PricePerGramCents)
		rows = append(rows, goldItemRow{GoldItem: it, Currency: cur, ValueCents: v})
		gd.TotalFineGrams += it.WeightGrams * service.Purity(it.PurityKarat) * float64(it.Quantity)
		gd.TotalValueCents += v
	}
	gd.Items = rows
	gd.TotalItems = len(rows)
	return gd, nil
}

func parseGoldItemForm(r *http.Request, householdID int64) (model.GoldItem, string) {
	r.ParseForm()
	it := model.GoldItem{HouseholdID: householdID}
	it.Name = r.PostFormValue("name")
	if it.Name == "" {
		return it, "Name is required."
	}
	weight, err := strconv.ParseFloat(r.PostFormValue("weight_grams"), 64)
	if err != nil || weight <= 0 {
		return it, "Weight must be a positive number of grams."
	}
	it.WeightGrams = weight
	karat, err := strconv.Atoi(r.PostFormValue("purity_karat"))
	if err != nil || (karat != 24 && karat != 22 && karat != 18 && karat != 14) {
		return it, "Choose a valid purity."
	}
	it.PurityKarat = karat
	qty, err := strconv.Atoi(r.PostFormValue("quantity"))
	if err != nil || qty <= 0 {
		qty = 1
	}
	it.Quantity = qty
	it.Location = r.PostFormValue("location")
	return it, ""
}
