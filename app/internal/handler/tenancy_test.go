package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// do sends an authenticated request, carrying the CSRF token the same way htmx
// does (the session token is the cookie value).
func do(t *testing.T, h http.Handler, method, path string, form url.Values, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	body := ""
	if form != nil {
		body = form.Encode()
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-CSRF-Token", c.Value)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// signup registers a fresh user, which creates a fresh household.
func signup(t *testing.T, h http.Handler, st *store.Store, email string) (*http.Cookie, int64) {
	t.Helper()
	w := post(t, h, "/signup", url.Values{
		"name": {email}, "email": {email}, "password": {"paripari123"},
	}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("signup %s: %d: %s", email, w.Code, w.Body.String())
	}
	u, err := st.UserByEmail(email)
	if err != nil {
		t.Fatalf("user %s: %v", email, err)
	}
	return sessionCookie(t, w), u.HouseholdID
}

// seedChildRows gives a household one income+deduction, one scenario+change
// and one trip+item, and returns their ids.
func seedChildRows(t *testing.T, st *store.Store, hhID, userID int64) (incomeID, dedID, scenarioID, changeID, tripID, itemID int64) {
	t.Helper()
	var err error
	incomeID, err = st.CreateIncome(&model.IncomeSource{
		HouseholdID: hhID, UserID: userID, Name: "Salary", Kind: "fixed",
		PayStructure: 12, GrossYearlyCents: 10_000_000, Currency: "CHF",
	})
	if err != nil {
		t.Fatal(err)
	}
	dedID, err = st.CreateDeduction(hhID, &model.IncomeDeduction{
		IncomeSourceID: incomeID, Name: "Tax", AmountCents: 100_000, Period: "monthly",
	})
	if err != nil {
		t.Fatal(err)
	}
	scenarioID, err = st.CreateScenario(&model.Scenario{HouseholdID: hhID, Name: "Plan"})
	if err != nil {
		t.Fatal(err)
	}
	rate := 0.07
	changeID, err = st.CreateScenarioChange(hhID, &model.ScenarioChange{
		ScenarioID: scenarioID, ChangeType: "return_rate", Label: "Optimistic", ValueNum: &rate,
	})
	if err != nil {
		t.Fatal(err)
	}
	tripID, err = st.CreateTrip(&model.TripPlan{HouseholdID: hhID, Name: "Japan", MonthsToSave: 10})
	if err != nil {
		t.Fatal(err)
	}
	itemID, err = st.CreateTripItem(hhID, &model.TripItem{
		TripPlanID: tripID, Name: "Flights", AmountCents: 180_000, Currency: "CHF",
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestChildRecordsAreTenantScoped(t *testing.T) {
	h, st := newServerStore(t)
	cookieA, hhA := signup(t, h, st, "a@example.com")
	_, hhB := signup(t, h, st, "b@example.com")
	if hhA == hhB {
		t.Fatal("signups shared a household")
	}
	userA, _ := st.UserByEmail("a@example.com")
	userB, _ := st.UserByEmail("b@example.com")

	incomeA, _, scenarioA, changeA, tripA, itemA := seedChildRows(t, st, hhA, userA.ID)
	incomeB, dedB, scenarioB, changeB, tripB, itemB := seedChildRows(t, st, hhB, userB.ID)

	incomeForm := func(userID int64, name string) url.Values {
		return url.Values{
			"user_id": {itoa(userID)}, "name": {name}, "kind": {"fixed"},
			"pay_structure": {"12"}, "currency": {"CHF"}, "gross_yearly": {"1000.00"},
			"deduction_name[]": {"Hijacked"}, "deduction_amount[]": {"1.00"},
			"deduction_period[]": {"monthly"},
		}
	}

	// --- attack: A rewrites B's income and its deductions ---
	if w := do(t, h, http.MethodPut, "/income/"+itoa(incomeB), incomeForm(userA.ID, "Pwned"), cookieA); w.Code != http.StatusNotFound {
		t.Errorf("PUT foreign income: got %d, want 404: %s", w.Code, w.Body.String())
	}
	dedsB, err := st.Deductions(hhB, incomeB)
	if err != nil {
		t.Fatal(err)
	}
	if len(dedsB) != 1 || dedsB[0].ID != dedB || dedsB[0].Name != "Tax" {
		t.Errorf("B's deductions were tampered with: %+v", dedsB)
	}
	srcB, err := st.Income(hhB, incomeB)
	if err != nil || srcB.Name != "Salary" {
		t.Errorf("B's income was tampered with: %+v (%v)", srcB, err)
	}

	// --- attack: A deletes B's scenario change through A's own scenario ---
	do(t, h, http.MethodDelete, "/scenarios/"+itoa(scenarioA)+"/changes/"+itoa(changeB), nil, cookieA)
	if changes, err := st.ScenarioChanges(hhB, scenarioB); err != nil || len(changes) != 1 {
		t.Errorf("B's scenario change was deleted: %+v (%v)", changes, err)
	}

	// --- attack: A updates and deletes B's trip item through A's own trip ---
	do(t, h, http.MethodPost, "/trips/"+itoa(tripA)+"/items/"+itoa(itemB),
		url.Values{"name": {"Pwned"}, "amount": {"1.00"}, "currency": {"CHF"}}, cookieA)
	do(t, h, http.MethodDelete, "/trips/"+itoa(tripA)+"/items/"+itoa(itemB), nil, cookieA)
	itemsB, err := st.TripItems(hhB, tripB)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemsB) != 1 || itemsB[0].ID != itemB || itemsB[0].Name != "Flights" {
		t.Errorf("B's trip item was tampered with: %+v", itemsB)
	}

	// --- the legitimate same-household paths still work ---
	if w := do(t, h, http.MethodPut, "/income/"+itoa(incomeA), incomeForm(userA.ID, "Raise"), cookieA); w.Code != http.StatusOK {
		t.Fatalf("PUT own income: got %d: %s", w.Code, w.Body.String())
	}
	deds, err := st.Deductions(hhA, incomeA)
	if err != nil || len(deds) != 1 || deds[0].Name != "Hijacked" {
		t.Errorf("own deductions not rewritten: %+v (%v)", deds, err)
	}
	if w := do(t, h, http.MethodDelete, "/scenarios/"+itoa(scenarioA)+"/changes/"+itoa(changeA), nil, cookieA); w.Code != http.StatusOK {
		t.Fatalf("DELETE own scenario change: got %d: %s", w.Code, w.Body.String())
	}
	if changes, _ := st.ScenarioChanges(hhA, scenarioA); len(changes) != 0 {
		t.Errorf("own scenario change survived: %+v", changes)
	}
	if w := do(t, h, http.MethodDelete, "/trips/"+itoa(tripA)+"/items/"+itoa(itemA), nil, cookieA); w.Code != http.StatusOK {
		t.Fatalf("DELETE own trip item: got %d: %s", w.Code, w.Body.String())
	}
	if items, _ := st.TripItems(hhA, tripA); len(items) != 0 {
		t.Errorf("own trip item survived: %+v", items)
	}
}

// Card transactions are scoped through the account that owns them. They had no
// handler reaching them with a raw id, so this was never exploitable — the test
// exists so the next route added cannot make it so.
func TestCCTransactionsAreTenantScoped(t *testing.T) {
	h, st := newServerStore(t)
	_, hhA := signup(t, h, st, "cc-a@example.com")
	_, hhB := signup(t, h, st, "cc-b@example.com")

	accA, err := st.CreateAccount(&model.Account{
		HouseholdID: hhA, Name: "A card", Currency: "CHF", BalanceCents: 100_000, Purpose: "cc_buffer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCCTransaction(hhA, &model.CCTransaction{
		AccountID: accA, Description: "Coffee", AmountCents: 450, Currency: "CHF", CashbackCents: 10,
	}); err != nil {
		t.Fatal(err)
	}

	// B cannot read A's rows through A's account id.
	if txs, err := st.CCTransactions(hhB, accA, 0); err != nil || len(txs) != 0 {
		t.Errorf("B read A's card transactions: %v, %v", txs, err)
	}
	if cb, err := st.CCCashbackTotal(hhB, accA); err != nil || cb != 0 {
		t.Errorf("B read A's cashback: %d, %v", cb, err)
	}
	// B cannot write into A's account.
	if _, err := st.CreateCCTransaction(hhB, &model.CCTransaction{
		AccountID: accA, Description: "Hijack", AmountCents: 999, Currency: "CHF",
	}); err != store.ErrNotFound {
		t.Errorf("B wrote into A's account: err = %v, want ErrNotFound", err)
	}
	// B cannot delete A's row.
	txs, err := st.CCTransactions(hhA, accA, 0)
	if err != nil || len(txs) != 1 {
		t.Fatalf("A's own rows: %v, %v", txs, err)
	}
	if err := st.DeleteCCTransaction(hhB, txs[0].ID); err != nil {
		t.Fatal(err)
	}
	// A's data is untouched throughout.
	after, err := st.CCTransactions(hhA, accA, 0)
	if err != nil || len(after) != 1 || after[0].Description != "Coffee" || after[0].CashbackCents != 10 {
		t.Errorf("A's transactions were tampered with: %+v", after)
	}
	if cb, err := st.CCCashbackTotal(hhA, accA); err != nil || cb != 10 {
		t.Errorf("A's cashback = %d, want 10 (%v)", cb, err)
	}
}
