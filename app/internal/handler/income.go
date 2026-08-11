package handler

import (
	"net/http"
	"strconv"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// incomeSourceView pairs a stored income source with its computed figures —
// the template never recomputes NetMonthly itself.
type incomeSourceView struct {
	Src  model.IncomeSource
	Calc service.IncomeCalc
}

type incomePartnerView struct {
	User                 model.User
	Sources              []incomeSourceView
	TotalNetMonthlyCents int64
	DisplayCurrency      string
}

type incomeData struct {
	A, B       incomePartnerView
	HasPartner bool
}

type incomeFormData struct {
	Src            model.IncomeSource
	GrossYearlyStr string
	IsNew          bool
	Error          string
	CSRF           string
	Currencies     []string
}

func newIncomeFormData(src model.IncomeSource) *incomeFormData {
	return &incomeFormData{Src: src, GrossYearlyStr: CentsToStr(src.GrossYearlyCents), Currencies: view.Currencies}
}

// registerIncome mounts the income section: per-partner income sources with
// inline htmx add/edit/delete.
func registerIncome(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /income", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, "could not load household", http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "income", &view.PageData{Data: buildIncomeData(in)})
	})

	mux.HandleFunc("GET /income/new", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		userID, _ := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
		f := newIncomeFormData(model.IncomeSource{UserID: userID, Kind: "fixed", PayStructure: 12, Currency: sess.Household.DisplayCurrency})
		f.IsNew, f.CSRF = true, sess.Token
		d.View.Partial(w, "partials/income-form", f)
	})

	mux.HandleFunc("GET /income/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		src, err := d.Store.Income(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f := newIncomeFormData(*src)
		f.CSRF = sess.Token
		d.View.Partial(w, "partials/income-form", f)
	})

	mux.HandleFunc("GET /income/{id}/view", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		src, err := d.Store.Income(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		d.View.Partial(w, "partials/income-card", incomeSourceView{Src: *src, Calc: service.NetMonthly(*src)})
	})

	mux.HandleFunc("POST /income", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		src, formErr := parseIncomeForm(r, sess)
		if formErr != "" {
			f := newIncomeFormData(src)
			f.IsNew, f.Error, f.CSRF = true, formErr, sess.Token
			w.WriteHeader(http.StatusUnprocessableEntity)
			d.View.Partial(w, "partials/income-form", f)
			return
		}
		id, err := d.Store.CreateIncome(&src)
		if err != nil {
			http.Error(w, "could not save income", http.StatusInternalServerError)
			return
		}
		src.ID = id
		if err := saveDeductions(d, id, r); err != nil {
			http.Error(w, "could not save deductions", http.StatusInternalServerError)
			return
		}
		src.Deductions, _ = d.Store.Deductions(id)

		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/income-card", incomeSourceView{Src: src, Calc: service.NetMonthly(src)})
		d.View.Partial(w, "partials/income-new-trigger", src.UserID)
		writeIncomeSummary(d, w, r, sess)
	})

	mux.HandleFunc("PUT /income/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		src, formErr := parseIncomeForm(r, sess)
		if formErr != "" {
			f := newIncomeFormData(src)
			f.Error, f.CSRF = formErr, sess.Token
			w.WriteHeader(http.StatusUnprocessableEntity)
			d.View.Partial(w, "partials/income-form", f)
			return
		}
		src.ID = id
		src.HouseholdID = sess.Household.ID
		if err := d.Store.UpdateIncome(&src); err != nil {
			http.Error(w, "could not save income", http.StatusInternalServerError)
			return
		}
		// Boring rebuild: drop and recreate deductions rather than diffing rows.
		existing, _ := d.Store.Deductions(id)
		for _, ded := range existing {
			d.Store.DeleteDeduction(ded.ID)
		}
		if err := saveDeductions(d, id, r); err != nil {
			http.Error(w, "could not save deductions", http.StatusInternalServerError)
			return
		}
		src.Deductions, _ = d.Store.Deductions(id)

		w.Header().Set("HX-Trigger", "pp:recalc")
		d.View.Partial(w, "partials/income-card", incomeSourceView{Src: src, Calc: service.NetMonthly(src)})
		writeIncomeSummary(d, w, r, sess)
	})

	mux.HandleFunc("DELETE /income/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := d.Store.DeleteIncome(sess.Household.ID, id); err != nil {
			http.Error(w, "could not delete income", http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		writeIncomeSummary(d, w, r, sess)
	})
}

func parseIncomeForm(r *http.Request, sess *auth.Session) (model.IncomeSource, string) {
	r.ParseForm()
	src := model.IncomeSource{HouseholdID: sess.Household.ID}
	userID, _ := strconv.ParseInt(r.PostFormValue("user_id"), 10, 64)
	src.UserID = userID
	if userID != sess.User.ID && (sess.Partner == nil || userID != sess.Partner.ID) {
		return src, "That partner doesn't belong to this household."
	}
	src.Name = r.PostFormValue("name")
	if src.Name == "" {
		return src, "Name is required."
	}
	src.Kind = r.PostFormValue("kind")
	if src.Kind != "variable" {
		src.Kind = "fixed"
	}
	src.PayStructure, _ = strconv.Atoi(r.PostFormValue("pay_structure"))
	if src.PayStructure != 13 {
		src.PayStructure = 12
	}
	src.Currency = r.PostFormValue("currency")
	if !ValidCurrency(src.Currency) {
		return src, "Choose a supported currency."
	}
	gross, err := ParseAmount(r.PostFormValue("gross_yearly"))
	if err != nil || gross < 0 {
		return src, "Gross yearly amount is not valid."
	}
	src.GrossYearlyCents = gross
	return src, ""
}

func saveDeductions(d *Deps, incomeID int64, r *http.Request) error {
	names := r.PostForm["deduction_name[]"]
	amounts := r.PostForm["deduction_amount[]"]
	periods := r.PostForm["deduction_period[]"]
	for i, name := range names {
		if name == "" {
			continue
		}
		var raw int64
		if i < len(amounts) {
			raw, _ = ParseAmount(amounts[i])
		}
		period := "monthly"
		if i < len(periods) {
			switch periods[i] {
			case "yearly", "percent":
				period = periods[i]
			}
		}
		ded := model.IncomeDeduction{IncomeSourceID: incomeID, Name: name, Period: period}
		if period == "percent" {
			// ParseAmount already scaled by 100, which is exactly basis points.
			ded.PercentBP = raw
		} else {
			ded.AmountCents = raw
		}
		if _, err := d.Store.CreateDeduction(&ded); err != nil {
			return err
		}
	}
	return nil
}

func writeIncomeSummary(d *Deps, w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	in, err := BuildInputs(d, r)
	if err != nil {
		return
	}
	d.View.Partial(w, "partials/income-summary", buildIncomeData(in))
}

func buildIncomeData(in service.Inputs) *incomeData {
	partnerIncomes := service.PartnerIncomes(in.Incomes, in.Rates, in.Display)
	build := func(u model.User) incomePartnerView {
		v := incomePartnerView{User: u, DisplayCurrency: in.Display}
		v.TotalNetMonthlyCents = partnerIncomes[u.ID].TotalMonthlyCents
		for _, s := range in.Incomes {
			if s.UserID == u.ID {
				v.Sources = append(v.Sources, incomeSourceView{Src: s, Calc: service.NetMonthly(s)})
			}
		}
		return v
	}
	return &incomeData{A: build(in.PartnerA), B: build(in.PartnerB), HasPartner: in.PartnerB.ID != 0}
}
