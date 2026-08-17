package handler

import (
	"github.com/mattmezza/paripari/internal/jobs"
	"net/http"
	"strconv"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// expenseRowView is one expense row, ready to render — shares already computed
// by the engine, never in the template.
type expenseRowView struct {
	Exp                      model.Expense
	AmountStr                string // plain decimal for the inline-edit input, e.g. "1234.50"
	OwnerName                string // personal rows only
	ShareACents, ShareBCents int64  // common rows only
	AccountName              string // budget-holder account, when the expense names one
}

type expenseGroupView struct {
	Category      string // personal | common
	Subcategory   string
	Rows          []expenseRowView
	SubtotalCents int64
}

// expensesData backs the whole page and the "expenses-list" partial that gets
// swapped on every mutation.
type expensesData struct {
	Filter                     string // all | common | personal
	Groups                     []expenseGroupView
	Currency                   string
	CSRF                       string
	HasPartner                 bool
	PartnerAID, PartnerBID     int64
	PartnerAName, PartnerBName string
}

// expensesSummaryView is the sticky-bar dot — sums precomputed here since
// templates get no arithmetic funcs.
type expensesSummaryView struct {
	Currency                   string
	CommonTotalCents           int64
	APersonalTotalCents        int64
	BPersonalTotalCents        int64
	AContributionCents         int64
	BContributionCents         int64
	AvailableCents             int64
	AAvailableCents            int64
	BAvailableCents            int64
	PartnerAName, PartnerBName string
}

func newExpensesSummaryView(ov service.MonthlyOverview) expensesSummaryView {
	return expensesSummaryView{
		Currency:            ov.Currency,
		CommonTotalCents:    ov.CommonExpensesCents + ov.CommonSavingsCents,
		APersonalTotalCents: ov.A.PersonalExpensesCents + ov.A.PersonalSavingsCents,
		BPersonalTotalCents: ov.B.PersonalExpensesCents + ov.B.PersonalSavingsCents,
		AContributionCents:  ov.A.TotalOutCents,
		BContributionCents:  ov.B.TotalOutCents,
		AvailableCents:      ov.AvailableCents,
		AAvailableCents:     ov.A.AvailableCents,
		BAvailableCents:     ov.B.AvailableCents,
		PartnerAName:        ov.A.Name,
		PartnerBName:        ov.B.Name,
	}
}

// expensesPageData is the full-page dot: the grouped list plus the initial
// sticky-summary render.
type expensesPageData struct {
	List    *expensesData
	Summary expensesSummaryView
}

type expenseFormData struct {
	Exp          model.Expense
	AmountStr    string
	IsNew        bool
	Error        string
	CSRF         string
	Currencies   []string
	PartnerAID   int64
	PartnerAName string
	PartnerBID   int64
	PartnerBName string
	HasPartner   bool
	Accounts     []model.Account
	Kinds        []struct{ Value, Label string }
	AccountID    int64 // 0 = household default
	OwnerIsB     bool  // Exp.UserID belongs to partner B — *int64 doesn't compare cleanly in templates
}

func registerExpenses(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /expenses", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, "could not load household", http.StatusInternalServerError)
			return
		}
		filter := r.URL.Query().Get("filter")
		data := buildExpensesData(in, filter)
		data.CSRF = auth.FromContext(r).Token
		d.View.Render(w, r, "expenses", &view.PageData{
			Data: expensesPageData{List: data, Summary: newExpensesSummaryView(service.BuildOverview(in))},
		})
	})

	// The analysis view is read-only: same expenses, grouped by subcategory
	// and laid out as flows instead of rows.
	mux.HandleFunc("GET /expenses/analysis", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, "could not load household", http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "expenses-analysis", &view.PageData{
			Title: "Expense analysis", Data: service.BuildBreakdown(in),
		})
	})

	mux.HandleFunc("GET /expenses/new", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		category := r.URL.Query().Get("category")
		if category != "personal" {
			category = "common"
		}
		exp := model.Expense{Category: category, Subcategory: r.URL.Query().Get("subcategory"), Currency: sess.Household.DisplayCurrency}
		if category == "personal" {
			uid := sess.User.ID
			exp.UserID = &uid
		}
		d.View.Partial(w, "partials/expenses-form", newExpenseFormData(exp, sess, true, householdAccounts(d, sess)))
	})

	mux.HandleFunc("GET /expenses/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		exp, err := d.Store.Expense(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		d.View.Partial(w, "partials/expenses-form", newExpenseFormData(*exp, sess, false, householdAccounts(d, sess)))
	})

	mux.HandleFunc("POST /expenses", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		exp, formErr := parseExpenseForm(r, sess, nil, householdAccounts(d, sess))
		if formErr != "" {
			f := newExpenseFormData(exp, sess, true, householdAccounts(d, sess))
			f.Error = formErr
			// The form's hx-target is the list it lives inside; on the error
			// branch it must replace itself, not the list.
			w.Header().Set("HX-Retarget", "#expenses-form-new")
			w.Header().Set("HX-Reswap", "outerHTML")
			w.WriteHeader(http.StatusUnprocessableEntity)
			d.View.Partial(w, "partials/expenses-form", f)
			return
		}
		if _, err := d.Store.CreateExpense(&exp); err != nil {
			http.Error(w, "could not save expense", http.StatusInternalServerError)
			return
		}
		writeExpensesList(d, w, r, "")
	})

	mux.HandleFunc("PUT /expenses/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		existing, err := d.Store.Expense(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		exp, formErr := parseExpenseForm(r, sess, existing, householdAccounts(d, sess))
		if formErr != "" {
			f := newExpenseFormData(exp, sess, false, householdAccounts(d, sess))
			f.Error = formErr
			w.Header().Set("HX-Retarget", "#expenses-form-"+strconv.FormatInt(id, 10))
			w.Header().Set("HX-Reswap", "outerHTML")
			w.WriteHeader(http.StatusUnprocessableEntity)
			d.View.Partial(w, "partials/expenses-form", f)
			return
		}
		exp.ID = id
		exp.HouseholdID = sess.Household.ID
		if err := d.Store.UpdateExpense(&exp); err != nil {
			http.Error(w, "could not save expense", http.StatusInternalServerError)
			return
		}
		writeExpensesList(d, w, r, "")
	})

	mux.HandleFunc("DELETE /expenses/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := d.Store.DeleteExpense(sess.Household.ID, id); err != nil {
			http.Error(w, "could not delete expense", http.StatusInternalServerError)
			return
		}
		writeExpensesList(d, w, r, "")
	})
}

// writeExpensesList re-renders the whole grouped list plus the sticky summary
// bar (OOB) and fires pp:recalc. Re-deriving the full list on every mutation
// is the boring, always-correct way to keep subtotals in sync — the household
// has at most a few dozen expenses, so recomputing all of them costs nothing.
func writeExpensesList(d *Deps, w http.ResponseWriter, r *http.Request, filter string) {
	// Every path here follows a mutation, so record the household's monthly
	// picture: /history charts income and expenses over time from these.
	if sess := auth.FromContext(r); sess != nil {
		jobs.SnapshotHousehold(r.Context(), sess.Household.ID)
	}
	in, err := BuildInputs(d, r)
	if err != nil {
		http.Error(w, "could not load household", http.StatusInternalServerError)
		return
	}
	if filter == "" {
		filter = r.URL.Query().Get("filter")
	}
	data := buildExpensesData(in, filter)
	data.CSRF = auth.FromContext(r).Token
	// Headers must be set before the first Write (which implicitly sends 200).
	w.Header().Set("HX-Trigger", "pp:recalc")
	d.View.Partial(w, "partials/expenses-list", data)
	d.View.Partial(w, "partials/expenses-summary", newExpensesSummaryView(service.BuildOverview(in)))
}

func newExpenseFormData(exp model.Expense, sess *auth.Session, isNew bool, accounts []model.Account) *expenseFormData {
	if exp.Kind == "" {
		exp.Kind = model.KindExpense
	}
	f := &expenseFormData{
		Exp: exp, AmountStr: CentsToStr(exp.AmountCents), IsNew: isNew, CSRF: sess.Token, Currencies: view.Currencies,
		PartnerAID: sess.User.ID, PartnerAName: sess.User.Name, Accounts: accounts, Kinds: model.ExpenseKinds,
	}
	if exp.AccountID != nil {
		f.AccountID = *exp.AccountID
	}
	if sess.Partner != nil {
		f.PartnerBID, f.PartnerBName, f.HasPartner = sess.Partner.ID, sess.Partner.Name, true
		f.OwnerIsB = exp.UserID != nil && *exp.UserID == sess.Partner.ID
	}
	return f
}

// householdAccounts lists the accounts an expense can be routed to. A store
// error just means "no accounts to choose from" — the select falls back to the
// household default, which is what a NULL account_id means anyway.
func householdAccounts(d *Deps, sess *auth.Session) []model.Account {
	accounts, err := d.Store.Accounts(sess.Household.ID)
	if err != nil {
		return nil
	}
	return accounts
}

func parseExpenseForm(r *http.Request, sess *auth.Session, existing *model.Expense, accounts []model.Account) (model.Expense, string) {
	r.ParseForm()
	exp := model.Expense{HouseholdID: sess.Household.ID}
	if existing != nil {
		exp = *existing
	}

	if r.PostForm.Has("name") {
		exp.Name = r.PostFormValue("name")
	}
	if exp.Name == "" {
		return exp, "Name is required."
	}
	if r.PostForm.Has("amount") {
		amt, err := ParseAmount(r.PostFormValue("amount"))
		if err != nil || amt < 0 {
			return exp, "Amount is not valid."
		}
		exp.AmountCents = amt
	}
	if r.PostForm.Has("currency") {
		exp.Currency = r.PostFormValue("currency")
	}
	if !ValidCurrency(exp.Currency) {
		return exp, "Choose a supported currency."
	}
	if r.PostForm.Has("subcategory") {
		exp.Subcategory = r.PostFormValue("subcategory")
	}
	if r.PostForm.Has("kind") {
		exp.Kind = r.PostFormValue("kind")
		if !model.ValidExpenseKind(exp.Kind) {
			return exp, "Choose a valid tag."
		}
	}
	// A "savings" subcategory with the tag left on "Regular expense" would
	// silently zero the dashboard's Savings figure — the subcategory wins.
	exp.Kind = model.KindForSubcategory(exp.Kind, exp.Subcategory)
	if r.PostForm.Has("account_id") {
		exp.AccountID = nil
		if v := r.PostFormValue("account_id"); v != "" {
			id, _ := strconv.ParseInt(v, 10, 64)
			for _, a := range accounts {
				if a.ID == id {
					exp.AccountID = &a.ID
					break
				}
			}
			if exp.AccountID == nil {
				return exp, "That account doesn't belong to this household."
			}
		}
	}
	if r.PostForm.Has("category") {
		exp.Category = r.PostFormValue("category")
		if exp.Category != "personal" {
			exp.Category = "common"
		}
	}
	if exp.Category == "personal" {
		if r.PostForm.Has("user_id") {
			uid, _ := strconv.ParseInt(r.PostFormValue("user_id"), 10, 64)
			if uid != sess.User.ID && (sess.Partner == nil || uid != sess.Partner.ID) {
				return exp, "That partner doesn't belong to this household."
			}
			exp.UserID = &uid
		}
		if exp.UserID == nil {
			return exp, "Choose who this expense belongs to."
		}
	} else {
		exp.UserID = nil
	}
	return exp, ""
}

func buildExpensesData(in service.Inputs, filter string) *expensesData {
	if filter != "common" && filter != "personal" {
		filter = "all"
	}
	incomes := service.PartnerIncomes(in.Incomes, in.Rates, in.Display)
	ratio := service.SplitRatio(in.Household, incomes, in.PartnerA.ID, in.PartnerB.ID)

	accountNames := map[int64]string{}
	for _, a := range in.Accounts {
		accountNames[a.ID] = a.Name
	}

	names := map[int64]string{in.PartnerA.ID: in.PartnerA.Name}
	if in.PartnerB.ID != 0 {
		names[in.PartnerB.ID] = in.PartnerB.Name
	}

	groups := map[string]*expenseGroupView{}
	var order []string
	for _, e := range in.Expenses {
		if filter != "all" && e.Category != filter {
			continue
		}
		key := e.Category + "|" + e.Subcategory
		g, ok := groups[key]
		if !ok {
			g = &expenseGroupView{Category: e.Category, Subcategory: e.Subcategory}
			groups[key] = g
			order = append(order, key)
		}
		// Convert each expense into the household display currency once, so the
		// subtotal and partner shares agree with BuildOverview (see #3). The
		// inline-edit input keeps the native amount below.
		amt := service.Convert(in.Rates, e.AmountCents, e.Currency, in.Display)
		row := expenseRowView{Exp: e, AmountStr: CentsToStr(e.AmountCents)}
		if e.AccountID != nil {
			row.AccountName = accountNames[*e.AccountID]
		}
		if e.Category == "common" {
			row.ShareACents, row.ShareBCents = service.ShareOf(amt, ratio)
		} else if e.UserID != nil {
			row.OwnerName = names[*e.UserID]
		}
		g.Rows = append(g.Rows, row)
		g.SubtotalCents += amt
	}

	data := &expensesData{
		Filter: filter, Currency: in.Display, HasPartner: in.PartnerB.ID != 0,
		PartnerAID: in.PartnerA.ID, PartnerAName: in.PartnerA.Name,
		PartnerBID: in.PartnerB.ID, PartnerBName: in.PartnerB.Name,
	}
	for _, key := range order {
		data.Groups = append(data.Groups, *groups[key])
	}
	return data
}
