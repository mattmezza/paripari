package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
)

// tripEnvelope gives a household a holiday envelope to fund trips from.
func tripEnvelope(t *testing.T, st *store.Store, hhID int64, cents int64) int64 {
	t.Helper()
	id, err := st.CreateAccount(&model.Account{
		HouseholdID: hhID, Name: "Holiday envelope", Currency: "CHF",
		BalanceCents: cents, Purpose: "envelope",
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// onlyTrip returns the household's single trip plan.
func onlyTrip(t *testing.T, st *store.Store, hhID int64) model.TripPlan {
	t.Helper()
	trips, err := st.Trips(hhID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) != 1 {
		t.Fatalf("want exactly one trip, got %d", len(trips))
	}
	return trips[0]
}

// TestTripFundingRoundTrip: both strategies survive create, edit and re-read,
// and the chosen account comes back with them.
func TestTripFundingRoundTrip(t *testing.T) {
	h, st := newServerStore(t)
	cookie, hh := signup(t, h, st, "trips@example.com")
	acc := tripEnvelope(t, st, hh, 800_000)

	if w := do(t, h, http.MethodPost, "/trips", url.Values{
		"name": {"Japan"}, "funding_strategy": {model.TripOneShot}, "funding_account_id": {itoa(acc)},
	}, cookie); w.Code != http.StatusSeeOther {
		t.Fatalf("POST /trips: %d: %s", w.Code, w.Body.String())
	}
	tp := onlyTrip(t, st, hh)
	if tp.FundingStrategy != model.TripOneShot || tp.FundingAccountID == nil || *tp.FundingAccountID != acc {
		t.Fatalf("created trip = %+v", tp)
	}

	// The detail page renders the choice back, prose included.
	w := do(t, h, http.MethodGet, "/trips/"+itoa(tp.ID), nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("GET trip: %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Holiday envelope") || !strings.Contains(w.Body.String(), "paid in one go") {
		t.Errorf("one-shot detail page does not explain the funding: %s", w.Body.String())
	}

	// Switch it to spread funding over 5 months, keeping the envelope.
	if w := do(t, h, http.MethodPost, "/trips/"+itoa(tp.ID), url.Values{
		"name": {"Japan"}, "months_to_save": {"5"},
		"funding_strategy": {model.TripSpread}, "funding_account_id": {itoa(acc)},
	}, cookie); w.Code != http.StatusOK {
		t.Fatalf("POST trip edit: %d: %s", w.Code, w.Body.String())
	}
	tp = onlyTrip(t, st, hh)
	if tp.FundingStrategy != model.TripSpread || tp.MonthsToSave != 5 || *tp.FundingAccountID != acc {
		t.Fatalf("edited trip = %+v", tp)
	}

	// And back to the household default.
	if w := do(t, h, http.MethodPost, "/trips/"+itoa(tp.ID), url.Values{
		"name": {"Japan"}, "months_to_save": {"5"}, "funding_strategy": {model.TripSpread},
		"funding_account_id": {""},
	}, cookie); w.Code != http.StatusOK {
		t.Fatalf("POST trip edit: %d: %s", w.Code, w.Body.String())
	}
	if tp = onlyTrip(t, st, hh); tp.FundingAccountID != nil {
		t.Errorf("account should be cleared: %+v", tp)
	}
}

// TestTripCommitByStrategy: spread funding becomes a "holidays" common expense
// naming the account; one-shot funding creates no expense at all.
func TestTripCommitByStrategy(t *testing.T) {
	tests := []struct {
		name       string
		strategy   string
		wantExpAmt int64 // 0 = no expense at all
	}{
		{"spread becomes a monthly expense", model.TripSpread, 100_000},
		{"one shot adds nothing recurring", model.TripOneShot, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, st := newServerStore(t)
			cookie, hh := signup(t, h, st, tc.strategy+"@example.com")
			acc := tripEnvelope(t, st, hh, 2_000_000)

			do(t, h, http.MethodPost, "/trips", url.Values{
				"name": {"Japan"}, "funding_strategy": {tc.strategy}, "funding_account_id": {itoa(acc)},
			}, cookie)
			tp := onlyTrip(t, st, hh)
			if w := do(t, h, http.MethodPost, "/trips/"+itoa(tp.ID), url.Values{
				"name": {"Japan"}, "months_to_save": {"10"},
				"funding_strategy": {tc.strategy}, "funding_account_id": {itoa(acc)},
			}, cookie); w.Code != http.StatusOK {
				t.Fatalf("edit: %d", w.Code)
			}
			if _, err := st.CreateTripItem(hh, &model.TripItem{
				TripPlanID: tp.ID, Name: "All in", AmountCents: 1_000_000, Currency: "CHF",
			}); err != nil {
				t.Fatal(err)
			}

			if w := do(t, h, http.MethodPost, "/trips/"+itoa(tp.ID)+"/commit", nil, cookie); w.Code != http.StatusOK {
				t.Fatalf("commit: %d: %s", w.Code, w.Body.String())
			}
			exps, err := st.Expenses(hh)
			if err != nil {
				t.Fatal(err)
			}
			tp = onlyTrip(t, st, hh)
			if !tp.Committed {
				t.Error("trip is not committed")
			}
			if tc.wantExpAmt == 0 {
				if len(exps) != 0 || tp.LinkedExpenseID != nil {
					t.Fatalf("one-shot created expenses %+v / link %v", exps, tp.LinkedExpenseID)
				}
				return
			}
			if len(exps) != 1 {
				t.Fatalf("want one expense, got %+v", exps)
			}
			e := exps[0]
			if e.AmountCents != tc.wantExpAmt || e.Subcategory != "holidays" || e.Category != "common" {
				t.Errorf("expense = %+v", e)
			}
			if e.AccountID == nil || *e.AccountID != acc {
				t.Errorf("expense does not name the funding account: %+v", e)
			}
			if tp.LinkedExpenseID == nil || *tp.LinkedExpenseID != e.ID {
				t.Fatalf("trip does not remember its expense: %+v", tp)
			}

			// Uncommitting removes exactly that expense again.
			if w := do(t, h, http.MethodPost, "/trips/"+itoa(tp.ID)+"/uncommit", nil, cookie); w.Code != http.StatusOK {
				t.Fatalf("uncommit: %d", w.Code)
			}
			if exps, _ := st.Expenses(hh); len(exps) != 0 {
				t.Errorf("funding expense survived uncommit: %+v", exps)
			}
		})
	}
}
