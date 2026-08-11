package handler

import (
	"errors"
	"net/http"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/store"
	"github.com/mattmezza/paripari/internal/view"
)

// transferRow adds the presentation bits the template needs on top of the
// engine's Transfer: a plain (no currency, no thousands separator) amount for
// copy-to-clipboard, and a precomputed delta label ("New" / "+CHF 120.00").
type transferRow struct {
	service.Transfer
	AmountPlain string
	DeltaLabel  string
}

type transfersData struct {
	CSRF      string
	Currency  string
	Rows      []transferRow
	Changed   bool
	Confirmed bool
}

func buildTransfersData(d *Deps, r *http.Request) (*transfersData, error) {
	sess := auth.FromContext(r)
	in, err := BuildInputs(d, r)
	if err != nil {
		return nil, err
	}
	ov := service.BuildOverview(in)
	plan := service.BuildTransfers(in, ov)

	payload := ""
	if last, err := d.Store.LastTransferConfirmation(sess.Household.ID); err == nil {
		payload = last.Payload
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	plan.Diff(payload)

	data := &transfersData{CSRF: sess.Token, Currency: plan.Currency, Changed: plan.Changed, Confirmed: plan.Confirmed}
	for _, row := range plan.Rows {
		tr := transferRow{Transfer: row, AmountPlain: plainAmount(row.AmountCents)}
		switch {
		case row.New:
			tr.DeltaLabel = "New"
		case row.DeltaCents != 0:
			delta, sign := row.DeltaCents, "+"
			if delta < 0 {
				delta, sign = -delta, "-"
			}
			tr.DeltaLabel = sign + service.Money(delta, plan.Currency)
		}
		data.Rows = append(data.Rows, tr)
	}
	return data, nil
}

// registerTransfers mounts the transfers section: prompt.md Key Screen 3, the
// "what to do" screen.
func registerTransfers(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /transfers", func(w http.ResponseWriter, r *http.Request) {
		data, err := buildTransfersData(d, r)
		if err != nil {
			http.Error(w, "could not load transfers", http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "transfers", &view.PageData{Title: "Transfers", Data: data})
	})

	// Confirming is infrequent and changes the nav's "changed" flag app-wide,
	// so a full reload (not a fragment swap) is the simplest correct thing.
	mux.HandleFunc("POST /transfers/confirm", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, "could not load household", http.StatusInternalServerError)
			return
		}
		plan := service.BuildTransfers(in, service.BuildOverview(in))
		payload, err := plan.Payload()
		if err != nil {
			http.Error(w, "could not confirm transfers", http.StatusInternalServerError)
			return
		}
		if err := d.Store.ConfirmTransfers(sess.Household.ID, payload); err != nil {
			http.Error(w, "could not confirm transfers", http.StatusInternalServerError)
			return
		}
		view.SetFlash(w, "Transfer amounts confirmed.")
		redirect(w, r, "/transfers")
	})
}
