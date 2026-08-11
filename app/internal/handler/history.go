package handler

import (
	"context"
	"encoding/csv"
	"net/http"
	"sort"
	"time"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/jobs"
	"github.com/mattmezza/paripari/internal/view"
)

// histRow is one date's merged picture: the net worth buckets and the monthly
// cash-flow figures that were in force that day, both in display currency.
// Either half may be missing (a date can carry only one kind of snapshot).
type histRow struct {
	Date        string `json:"d"`
	HasNetWorth bool   `json:"-"`
	Liquid      int64  `json:"liquid"`
	Alternative int64  `json:"alternative"`
	RealEstate  int64  `json:"realEstate"`
	Total       int64  `json:"total"`
	HasFinance  bool   `json:"-"`
	Income      int64  `json:"income"`
	Expenses    int64  `json:"expenses"`
	Savings     int64  `json:"savings"`
	Available   int64  `json:"available"`
	Common      int64  `json:"common"`
	Personal    int64  `json:"personal"`
}

type histPayload struct {
	Currency string    `json:"currency"`
	NetWorth []histRow `json:"netWorth"`
	Finance  []histRow `json:"finance"`
}

type histData struct {
	Currency    string
	Range       string
	RangeLabel  string
	Rows        []histRow // most recent first, for the table
	HasNetWorth bool      // ≥2 net worth points
	HasFinance  bool      // ≥2 finance points
	Empty       bool      // nothing worth charting yet
	Today       histRow   // today's figures, for the empty state
	Payload     histPayload
}

// histRanges maps the selector value to a lookback and its label.
var histRanges = map[string]struct {
	Days  int
	Label string
}{
	"3m":  {90, "3 months"},
	"12m": {365, "12 months"},
	"all": {0, "All time"},
}

// registerHistory mounts the history screen: how net worth and the monthly
// cash-flow picture have moved over time, plus a CSV of the same rows.
func registerHistory(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /history", func(w http.ResponseWriter, r *http.Request) {
		hd, err := buildHistoryData(d, r)
		if err != nil {
			http.Error(w, "could not load history", http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "history", &view.PageData{
			Title:       "History",
			Description: "How your net worth and monthly picture have moved over time.",
			Data:        hd,
		})
	})

	mux.HandleFunc("GET /history.csv", func(w http.ResponseWriter, r *http.Request) {
		hd, err := buildHistoryData(d, r)
		if err != nil {
			http.Error(w, "could not load history", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="paripari-history.csv"`)
		cw := csv.NewWriter(w)
		defer cw.Flush()
		cw.Write([]string{"date", "currency", "net_worth_total", "liquid", "alternative",
			"real_estate", "monthly_income", "monthly_expenses", "monthly_savings",
			"monthly_available", "common_expenses", "personal_expenses"})
		// Oldest first in the file: spreadsheets chart that way round.
		for i := len(hd.Rows) - 1; i >= 0; i-- {
			row := hd.Rows[i]
			cw.Write([]string{row.Date, hd.Currency,
				CentsToStr(row.Total), CentsToStr(row.Liquid), CentsToStr(row.Alternative),
				CentsToStr(row.RealEstate), CentsToStr(row.Income), CentsToStr(row.Expenses),
				CentsToStr(row.Savings), CentsToStr(row.Available),
				CentsToStr(row.Common), CentsToStr(row.Personal)})
		}
	})
}

func buildHistoryData(d *Deps, r *http.Request) (*histData, error) {
	sess := auth.FromContext(r)
	hh := sess.Household
	cur := netWorthCurrency(r, hh)

	// ponytail: snapshot on view rather than hooking every income/expense
	// mutation handler — the user only notices freshness when they look at
	// history, and this touches one file instead of six.
	jobs.SnapshotHousehold(context.Background(), hh.ID)

	key := r.URL.Query().Get("range")
	rg, ok := histRanges[key]
	if !ok {
		key, rg = "12m", histRanges["12m"]
	}
	since := ""
	if rg.Days > 0 {
		since = time.Now().UTC().AddDate(0, 0, -rg.Days).Format("2006-01-02")
	}

	nwSnaps, err := d.Store.SnapshotsSince(hh.ID, since)
	if err != nil {
		return nil, err
	}
	finSnaps, err := d.Store.FinancialSnapshots(hh.ID, since)
	if err != nil {
		return nil, err
	}

	conv := func(cents int64) int64 { return d.Rates.Convert(cents, "CHF", cur) }

	byDate := map[string]*histRow{}
	get := func(date string) *histRow {
		if row, ok := byDate[date]; ok {
			return row
		}
		row := &histRow{Date: date}
		byDate[date] = row
		return row
	}
	var nwPoints, finPoints []histRow
	for _, s := range nwSnaps {
		row := get(s.Date)
		row.HasNetWorth = true
		row.Liquid, row.Alternative, row.RealEstate = conv(s.LiquidCents), conv(s.AlternativeCents), conv(s.RealEstateCents)
		row.Total = row.Liquid + row.Alternative + row.RealEstate
		nwPoints = append(nwPoints, *row)
	}
	for _, s := range finSnaps {
		row := get(s.Date)
		row.HasFinance = true
		row.Income, row.Expenses = conv(s.IncomeCents), conv(s.ExpensesCents)
		row.Savings, row.Available = conv(s.SavingsCents), conv(s.AvailableCents)
		row.Common, row.Personal = conv(s.CommonExpensesCents), conv(s.PersonalExpensesCents)
		finPoints = append(finPoints, *row)
	}

	// Merge into one table, most recent first. Both source slices are already
	// sorted by date, so walking them backwards and merging keeps that order.
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates))) // ISO dates sort as strings
	rows := make([]histRow, 0, len(dates))
	for _, date := range dates {
		rows = append(rows, *byDate[date])
	}

	hd := &histData{
		Currency: cur, Range: key, RangeLabel: rg.Label, Rows: rows,
		HasNetWorth: len(nwPoints) >= 2,
		HasFinance:  len(finPoints) >= 2,
		Payload:     histPayload{Currency: cur, NetWorth: nwPoints, Finance: finPoints},
	}
	hd.Empty = !hd.HasNetWorth && !hd.HasFinance
	if len(rows) > 0 {
		hd.Today = rows[0]
	}
	return hd, nil
}
