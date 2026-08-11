package handler_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
)

// upload posts a backup file the way the settings form does (htmx multipart +
// CSRF header). confirm mirrors the "I understand" checkbox.
func upload(t *testing.T, h http.Handler, body []byte, confirm bool, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if confirm {
		if err := mw.WriteField("confirm", "on"); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile("file", "backup.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	r := httptest.NewRequest(http.MethodPost, "/settings/import", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("X-CSRF-Token", c.Value)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func exportBytes(t *testing.T, h http.Handler, c *http.Cookie) []byte {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/settings/export", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /settings/export: %d: %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd == "" || cd[:10] != "attachment" {
		t.Errorf("Content-Disposition = %q", cd)
	}
	return w.Body.Bytes()
}

// snapshot is the household's data as the store sees it, with the volatile
// export timestamp cleared so two snapshots compare cleanly.
func snapshot(t *testing.T, st *store.Store, hhID int64) *store.Backup {
	t.Helper()
	b, err := st.ExportHousehold(hhID)
	if err != nil {
		t.Fatal(err)
	}
	b.ExportedAt = ""
	return b
}

type tally struct {
	Incomes, Deductions, Expenses, Accounts, CC, Assets, Gold, Goals int
	Scenarios, Changes, Trips, Items, NetWorth, Financial            int
}

func count(b *store.Backup) tally {
	var c tally
	c.Incomes = len(b.IncomeSources)
	for _, in := range b.IncomeSources {
		c.Deductions += len(in.Deductions)
	}
	c.Expenses, c.Accounts, c.CC = len(b.Expenses), len(b.Accounts), len(b.CCTransactions)
	c.Assets, c.Gold, c.Goals = len(b.Assets), len(b.GoldItems), len(b.Goals)
	c.Scenarios = len(b.Scenarios)
	for _, s := range b.Scenarios {
		c.Changes += len(s.Changes)
	}
	c.Trips = len(b.TripPlans)
	for _, tp := range b.TripPlans {
		c.Items += len(tp.Items)
	}
	c.NetWorth, c.Financial = len(b.NetWorthSnapshots), len(b.FinancialSnapshots)
	return c
}

// seedBackupData fills a household with one of everything, wired together:
// a personal expense pointing at an envelope account, an income source with two
// deductions, a scenario change targeting the expense.
func seedBackupData(t *testing.T, st *store.Store, hhID, userID int64) {
	t.Helper()
	newAcc := func(name, purpose string, cents int64, owner *int64) int64 {
		id, err := st.CreateAccount(&model.Account{
			HouseholdID: hhID, UserID: owner, Name: name, Currency: "CHF",
			BalanceCents: cents, Purpose: purpose,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	checking := newAcc("Joint checking", "checking", 500_000, nil)
	envelope := newAcc("Groceries envelope", "envelope", 25_000, nil)
	buffer := newAcc("Card buffer", "cc_buffer", 90_000, &userID)

	if _, err := st.CreateCCTransaction(hhID, &model.CCTransaction{
		AccountID: buffer, Description: "Coffee", AmountCents: 4_50, Currency: "CHF", CashbackCents: 10,
	}); err != nil {
		t.Fatal(err)
	}

	incomeID, err := st.CreateIncome(&model.IncomeSource{
		HouseholdID: hhID, UserID: userID, Name: "Salary", Kind: "fixed",
		PayStructure: 13, GrossYearlyCents: 12_345_600, Currency: "CHF",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []model.IncomeDeduction{
		{IncomeSourceID: incomeID, Name: "AHV", Period: "percent", PercentBP: 530},
		{IncomeSourceID: incomeID, Name: "Pension", AmountCents: 45_000, Period: "monthly"},
	} {
		if _, err := st.CreateDeduction(hhID, &d); err != nil {
			t.Fatal(err)
		}
	}

	expID, err := st.CreateExpense(&model.Expense{
		HouseholdID: hhID, Name: "Groceries", AmountCents: 78_950, Currency: "CHF",
		Category: "common", Subcategory: "food", AccountID: &envelope,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateExpense(&model.Expense{
		HouseholdID: hhID, Name: "Gym", AmountCents: 9_900, Currency: "CHF",
		Category: "personal", UserID: &userID, AccountID: &checking,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CreateAsset(&model.Asset{
		HouseholdID: hhID, Name: "Flat", Kind: "real_estate", EstimatedValueCents: 75_000_000, Currency: "CHF",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateGoldItem(&model.GoldItem{
		HouseholdID: hhID, Name: "Coin", WeightGrams: 31.1, PurityKarat: 24, Quantity: 2, Location: "safe",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateGoal(&model.Goal{
		HouseholdID: hhID, Name: "Emergency fund", TargetAmountCents: 2_000_000, Currency: "CHF",
	}); err != nil {
		t.Fatal(err)
	}

	scID, err := st.CreateScenario(&model.Scenario{HouseholdID: hhID, Name: "Tighter budget"})
	if err != nil {
		t.Fatal(err)
	}
	cheaper := int64(50_000)
	if _, err := st.CreateScenarioChange(hhID, &model.ScenarioChange{
		ScenarioID: scID, ChangeType: "expense_amount", TargetID: &expID,
		Label: "Groceries", ValueCents: &cheaper, Currency: "CHF",
	}); err != nil {
		t.Fatal(err)
	}

	tripID, err := st.CreateTrip(&model.TripPlan{HouseholdID: hhID, Name: "Japan", MonthsToSave: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTripItem(hhID, &model.TripItem{
		TripPlanID: tripID, Name: "Flights", AmountCents: 180_000, Currency: "CHF",
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.PutSnapshot(&model.NetWorthSnapshot{
		HouseholdID: hhID, Date: "2026-01-01", LiquidCents: 615_000, RealEstateCents: 75_000_000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutFinancialSnapshot(&model.FinancialSnapshot{
		HouseholdID: hhID, Date: "2026-01-01", Currency: "CHF",
		IncomeCents: 900_000, ExpensesCents: 88_850, AvailableCents: 811_150,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRoundTrip(t *testing.T) {
	h, st := newServerStore(t)
	c, hh := signup(t, h, st, "a@example.com")
	u, _ := st.UserByEmail("a@example.com")
	seedBackupData(t, st, hh, u.ID)

	before := snapshot(t, st, hh)
	want := tally{Incomes: 1, Deductions: 2, Expenses: 2, Accounts: 3, CC: 1, Assets: 1, Gold: 1,
		Goals: 1, Scenarios: 1, Changes: 1, Trips: 1, Items: 1, NetWorth: 1, Financial: 1}
	if got := count(before); got != want {
		t.Fatalf("seed exported as %+v, want %+v", got, want)
	}
	file := exportBytes(t, h, c)

	if w := upload(t, h, file, true, c); w.Code != http.StatusOK {
		t.Fatalf("import: %d: %s", w.Code, w.Body.String())
	}

	after := snapshot(t, st, hh)
	if got, want := count(after), count(before); got != want {
		t.Fatalf("counts after round trip = %+v, want %+v", got, want)
	}

	// Household settings survive.
	if after.Household != before.Household {
		t.Errorf("household settings = %+v, want %+v", after.Household, before.Household)
	}
	// Money is intact, to the cent.
	var groceries *model.Expense
	for i, e := range after.Expenses {
		if e.Name == "Groceries" {
			groceries = &after.Expenses[i]
		}
	}
	if groceries == nil || groceries.AmountCents != 78_950 {
		t.Fatalf("groceries expense = %+v, want 78950 cents", groceries)
	}
	// The expense -> account foreign key still points at the right account.
	if groceries.AccountID == nil {
		t.Fatal("groceries lost its budget account")
	}
	var accName string
	for _, a := range after.Accounts {
		if a.ID == *groceries.AccountID {
			accName = a.Name
		}
	}
	if accName != "Groceries envelope" {
		t.Errorf("groceries points at account %q, want %q", accName, "Groceries envelope")
	}
	// The income source keeps its deductions.
	if len(after.IncomeSources) != 1 {
		t.Fatalf("income sources = %d", len(after.IncomeSources))
	}
	in := after.IncomeSources[0]
	if in.GrossYearlyCents != 12_345_600 || in.PayStructure != 13 || in.UserID != u.ID {
		t.Errorf("income = %+v", in)
	}
	if len(in.Deductions) != 2 {
		t.Fatalf("deductions = %+v", in.Deductions)
	}
	for _, d := range in.Deductions {
		if d.IncomeSourceID != in.ID {
			t.Errorf("deduction %q hangs off income %d, want %d", d.Name, d.IncomeSourceID, in.ID)
		}
		if d.Name == "AHV" && (d.Period != "percent" || d.PercentBP != 530) {
			t.Errorf("AHV deduction = %+v", d)
		}
		if d.Name == "Pension" && d.AmountCents != 45_000 {
			t.Errorf("pension deduction = %+v", d)
		}
	}
	// The scenario change still targets the (new) groceries expense.
	if len(after.Scenarios) != 1 || len(after.Scenarios[0].Changes) != 1 {
		t.Fatalf("scenarios = %+v", after.Scenarios)
	}
	ch := after.Scenarios[0].Changes[0]
	if ch.TargetID == nil || *ch.TargetID != groceries.ID {
		t.Errorf("scenario change targets %v, want expense %d", ch.TargetID, groceries.ID)
	}
}

func TestBackupImportIsTenantScoped(t *testing.T) {
	h, st := newServerStore(t)
	cookieA, hhA := signup(t, h, st, "a@example.com")
	cookieB, hhB := signup(t, h, st, "b@example.com")
	userA, _ := st.UserByEmail("a@example.com")
	seedBackupData(t, st, hhA, userA.ID)

	beforeA := snapshot(t, st, hhA)
	file := exportBytes(t, h, cookieA)

	if w := upload(t, h, file, true, cookieB); w.Code != http.StatusOK {
		t.Fatalf("B imports A's file: %d: %s", w.Code, w.Body.String())
	}

	// A is untouched: same ids, same values.
	if afterA := snapshot(t, st, hhA); !reflect.DeepEqual(afterA, beforeA) {
		t.Errorf("A's data changed.\n before: %+v\n  after: %+v", beforeA, afterA)
	}

	// B got its own copy, on its own ids, owned by B's household.
	afterB := snapshot(t, st, hhB)
	if got, want := count(afterB), count(beforeA); got != want {
		t.Fatalf("B's counts = %+v, want %+v", got, want)
	}
	for _, e := range afterB.Expenses {
		if e.HouseholdID != hhB {
			t.Errorf("B's expense %q belongs to household %d", e.Name, e.HouseholdID)
		}
		for _, ae := range beforeA.Expenses {
			if e.ID == ae.ID {
				t.Errorf("B reused A's expense id %d", e.ID)
			}
		}
	}
	// A's only partner maps onto B's only partner: personal rows land on B's user.
	userB, _ := st.UserByEmail("b@example.com")
	if len(afterB.IncomeSources) != 1 || afterB.IncomeSources[0].UserID != userB.ID {
		t.Errorf("B's income owner = %+v, want user %d", afterB.IncomeSources, userB.ID)
	}
	for _, e := range afterB.Expenses {
		if e.Category == "personal" && (e.UserID == nil || *e.UserID != userB.ID) {
			t.Errorf("B's personal expense %q owned by %v, want %d", e.Name, e.UserID, userB.ID)
		}
	}
}

func TestBackupImportIsAtomic(t *testing.T) {
	h, st := newServerStore(t)
	c, hh := signup(t, h, st, "a@example.com")
	u, _ := st.UserByEmail("a@example.com")
	seedBackupData(t, st, hh, u.ID)
	before := snapshot(t, st, hh)
	file := exportBytes(t, h, c)

	// Valid JSON, invalid payload: the third account has a bogus purpose.
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(file))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	accounts := doc["Accounts"].([]any)
	if len(accounts) < 3 {
		t.Fatalf("need at least 3 accounts to break the third, got %d", len(accounts))
	}
	accounts[2].(map[string]any)["Purpose"] = "mattress"
	broken, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	w := upload(t, h, broken, true, c)
	if w.Code == http.StatusOK {
		t.Fatalf("import with a bad purpose was accepted")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("mattress")) {
		t.Errorf("error should name the bad value: %s", w.Body.String())
	}
	if after := snapshot(t, st, hh); !reflect.DeepEqual(after, before) {
		t.Errorf("failed import changed the data.\n before: %+v\n  after: %+v", before, after)
	}

	// A failure the validator cannot see (duplicate snapshot dates trip a UNIQUE
	// constraint mid-import) must roll the transaction back just as cleanly.
	snaps := doc["NetWorthSnapshots"].([]any)
	doc["Accounts"].([]any)[2].(map[string]any)["Purpose"] = "checking"
	doc["NetWorthSnapshots"] = append(snaps, snaps[0])
	dup, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if w := upload(t, h, dup, true, c); w.Code == http.StatusOK {
		t.Fatalf("duplicate snapshot dates were accepted")
	}
	if after := snapshot(t, st, hh); !reflect.DeepEqual(after, before) {
		t.Errorf("mid-transaction failure left the data changed.\n before: %+v\n  after: %+v", before, after)
	}
}

func TestBackupUnmappedPartnerIsReported(t *testing.T) {
	h, st := newServerStore(t)
	cookieA, hhA := signup(t, h, st, "a@example.com")
	cookieB, hhB := signup(t, h, st, "b@example.com")
	userA, _ := st.UserByEmail("a@example.com")
	seedBackupData(t, st, hhA, userA.ID)

	// Pretend A had a second partner owning a personal expense and an income.
	var b store.Backup
	if err := json.Unmarshal(exportBytes(t, h, cookieA), &b); err != nil {
		t.Fatal(err)
	}
	ghost := int64(9999)
	b.Partners = append(b.Partners, store.BackupPartner{ID: ghost, Name: "Ghost", Email: "ghost@example.com"})
	b.Expenses = append(b.Expenses, model.Expense{
		ID: 9001, Name: "Ghost gym", AmountCents: 5_000, Currency: "CHF", Category: "personal", UserID: &ghost,
	})
	b.IncomeSources = append(b.IncomeSources, model.IncomeSource{
		ID: 9002, UserID: ghost, Name: "Ghost salary", Kind: "fixed",
		PayStructure: 12, GrossYearlyCents: 1_000_000, Currency: "CHF",
	})
	file, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}

	// B has one partner: what maps is imported, what doesn't is reported.
	w := upload(t, h, file, true, cookieB)
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d: %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("2 row(s) belonged to a partner")) {
		t.Errorf("unmapped rows not reported: %s", w.Body.String())
	}
	after := snapshot(t, st, hhB)
	for _, e := range after.Expenses {
		if e.Name == "Ghost gym" {
			t.Error("an expense with no valid owner was imported anyway")
		}
	}
	if len(after.IncomeSources) != 1 {
		t.Errorf("income sources = %d, want 1 (the ghost's is unownable)", len(after.IncomeSources))
	}
}

func TestBackupImportRejections(t *testing.T) {
	h, st := newServerStore(t)
	c, hh := signup(t, h, st, "a@example.com")
	u, _ := st.UserByEmail("a@example.com")
	seedBackupData(t, st, hh, u.ID)
	before := snapshot(t, st, hh)
	file := exportBytes(t, h, c)

	// No confirmation checkbox: refused, whatever the client sent.
	w := upload(t, h, file, false, c)
	if w.Code == http.StatusOK {
		t.Errorf("import without confirmation was accepted")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("confirmation box")) {
		t.Errorf("unhelpful refusal: %s", w.Body.String())
	}

	// Truncated file: readable error, nothing changed.
	w = upload(t, h, file[:len(file)/2], true, c)
	if w.Code == http.StatusOK {
		t.Errorf("truncated file was accepted")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("isn&#39;t valid JSON")) {
		t.Errorf("unhelpful parse error: %s", w.Body.String())
	}

	// A version from the future is refused rather than half-read.
	future := bytes.Replace(file, []byte(`"Version": 1`), []byte(`"Version": 99`), 1)
	if bytes.Equal(future, file) {
		t.Fatal("could not rewrite the version field")
	}
	w = upload(t, h, future, true, c)
	if w.Code == http.StatusOK {
		t.Errorf("version 99 file was accepted")
	}

	if after := snapshot(t, st, hh); !reflect.DeepEqual(after, before) {
		t.Errorf("a refused import changed the data")
	}
}
