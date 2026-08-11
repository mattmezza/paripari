package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/jobs"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/view"
)

// purposeOrder is the display order for account groups. cc_buffer accounts
// are rendered as their own special card (see cc-card.html), not in a group.
var purposeOrder = []string{"checking", "savings", "investment", "envelope", "pension"}

var purposeLabel = map[string]string{
	"checking":   "Checking",
	"savings":    "Savings",
	"investment": "Investment",
	"envelope":   "Budget envelopes",
	"pension":    "Pension",
	"cc_buffer":  "Credit card buffer",
}

// acctRow is one account plus its owner's display name ("" for a household
// account).
type acctRow struct {
	model.Account
	OwnerName    string
	OwnerID      int64 // 0 = household account; else PartnerA.ID or PartnerB.ID
	BalanceInput string // plain "1234.50" for the inline-editable balance field
}

type acctGroup struct {
	Purpose    string
	Label      string
	Accounts   []acctRow
	TotalCents int64
}

type ccRow struct {
	Account            model.Account
	Transactions       []model.CCTransaction
	CashbackTotalCents int64
}

// accountsData is the dot for both the full page and the accounts-body
// fragment every mutation swaps back in.
type accountsData struct {
	CSRF               string
	Display            string
	Currencies         []string
	PartnerA, PartnerB model.User
	Groups             []acctGroup
	CC                 []ccRow
	GrandTotalCents    int64
	Err                string
}

func plainAmount(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	s := strconv.FormatInt(cents/100, 10) + "." + pad2(cents%100)
	if neg {
		s = "-" + s
	}
	return s
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

func ownerName(sess *auth.Session, uid *int64) string {
	if uid == nil {
		return ""
	}
	if sess.User != nil && sess.User.ID == *uid {
		return sess.User.Name
	}
	if sess.Partner != nil && sess.Partner.ID == *uid {
		return sess.Partner.Name
	}
	return ""
}

func buildAccountsData(d *Deps, r *http.Request) (*accountsData, error) {
	sess := auth.FromContext(r)
	hh := sess.Household
	accounts, err := d.Store.Accounts(hh.ID)
	if err != nil {
		return nil, err
	}

	data := &accountsData{CSRF: sess.Token, Display: hh.DisplayCurrency, Currencies: view.Currencies}
	if sess.User != nil {
		data.PartnerA = *sess.User
	}
	if sess.Partner != nil {
		data.PartnerB = *sess.Partner
	}

	byPurpose := map[string][]acctRow{}
	for _, a := range accounts {
		data.GrandTotalCents += d.Rates.Convert(a.BalanceCents, a.Currency, hh.DisplayCurrency)
		if a.Purpose == "cc_buffer" {
			tx, err := d.Store.CCTransactions(a.ID, 10)
			if err != nil {
				return nil, err
			}
			cb, err := d.Store.CCCashbackTotal(a.ID)
			if err != nil {
				return nil, err
			}
			data.CC = append(data.CC, ccRow{Account: a, Transactions: tx, CashbackTotalCents: cb})
			continue
		}
		var ownerID int64
		if a.UserID != nil {
			ownerID = *a.UserID
		}
		byPurpose[a.Purpose] = append(byPurpose[a.Purpose], acctRow{
			Account: a, OwnerName: ownerName(sess, a.UserID), OwnerID: ownerID, BalanceInput: plainAmount(a.BalanceCents),
		})
	}

	for _, p := range purposeOrder {
		rows := byPurpose[p]
		if len(rows) == 0 {
			continue
		}
		var tot int64
		for _, row := range rows {
			tot += d.Rates.Convert(row.BalanceCents, row.Currency, hh.DisplayCurrency)
		}
		data.Groups = append(data.Groups, acctGroup{Purpose: p, Label: purposeLabel[p], Accounts: rows, TotalCents: tot})
	}
	return data, nil
}

// renderAccountsBody re-runs buildAccountsData and swaps the #accounts-body
// fragment, firing pp:recalc for every screen that shows an account balance.
func renderAccountsBody(d *Deps, w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	data, err := buildAccountsData(d, r)
	if err != nil {
		http.Error(w, "could not load accounts", http.StatusInternalServerError)
		return
	}
	data.Err = errMsg
	w.Header().Set("HX-Trigger", "pp:recalc")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	d.View.Partial(w, "partials/accounts-body", data)
}

func registerAccounts(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {
		data, err := buildAccountsData(d, r)
		if err != nil {
			http.Error(w, "could not load accounts", http.StatusInternalServerError)
			return
		}
		d.View.Render(w, r, "accounts", &view.PageData{Title: "Accounts", Data: data})
	})

	mux.HandleFunc("POST /accounts", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		name := strings.TrimSpace(r.PostFormValue("name"))
		purpose := r.PostFormValue("purpose")
		currency := r.PostFormValue("currency")
		balance, err := ParseAmount(orZero(r.PostFormValue("balance")))
		if name == "" || !ValidPurpose(purpose) || !ValidCurrency(currency) || err != nil {
			renderAccountsBody(d, w, r, http.StatusUnprocessableEntity, "Fill in a name, a valid purpose and currency, and a valid balance.")
			return
		}
		a := &model.Account{
			HouseholdID:  sess.Household.ID,
			Name:         name,
			Institution:  strings.TrimSpace(r.PostFormValue("institution")),
			IBAN:         strings.TrimSpace(r.PostFormValue("iban")),
			Currency:     currency,
			BalanceCents: balance,
			Purpose:      purpose,
		}
		if uid := parseOwnerID(sess, r.PostFormValue("user_id")); uid != nil {
			a.UserID = uid
		}
		if _, err := d.Store.CreateAccount(a); err != nil {
			http.Error(w, "could not create account", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(r.Context(), sess.Household.ID)
		renderAccountsBody(d, w, r, http.StatusOK, "")
	})

	mux.HandleFunc("POST /accounts/{id}/balance", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		acc, err := d.Store.Account(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		cents, err := ParseAmount(r.PostFormValue("balance"))
		if err != nil {
			renderAccountsBody(d, w, r, http.StatusUnprocessableEntity, "That balance doesn't look like a valid amount.")
			return
		}
		acc.BalanceCents = cents
		if err := d.Store.UpdateAccount(acc); err != nil {
			http.Error(w, "could not update balance", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(r.Context(), sess.Household.ID) // snapshot-on-edit hook
		renderAccountsBody(d, w, r, http.StatusOK, "")
	})

	mux.HandleFunc("POST /accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		acc, err := d.Store.Account(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimSpace(r.PostFormValue("name"))
		purpose := r.PostFormValue("purpose")
		currency := r.PostFormValue("currency")
		if name == "" || !ValidPurpose(purpose) || !ValidCurrency(currency) {
			renderAccountsBody(d, w, r, http.StatusUnprocessableEntity, "Fill in a name, a valid purpose and a valid currency.")
			return
		}
		acc.Name = name
		acc.Institution = strings.TrimSpace(r.PostFormValue("institution"))
		acc.IBAN = strings.TrimSpace(r.PostFormValue("iban"))
		acc.Currency = currency
		acc.Purpose = purpose
		acc.UserID = parseOwnerID(sess, r.PostFormValue("user_id"))
		if err := d.Store.UpdateAccount(acc); err != nil {
			http.Error(w, "could not update account", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(r.Context(), sess.Household.ID)
		renderAccountsBody(d, w, r, http.StatusOK, "")
	})

	mux.HandleFunc("DELETE /accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := d.Store.DeleteAccount(sess.Household.ID, id); err != nil {
			http.Error(w, "could not delete account", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(r.Context(), sess.Household.ID)
		renderAccountsBody(d, w, r, http.StatusOK, "")
	})

	registerCC(mux, d)
}

// registerCC mounts the credit-card discipline workflow under /accounts/cc/*
// (no dedicated cc section exists in router.go, so it lives with accounts per
// prompt.md's file-ownership fallback).
func registerCC(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("POST /accounts/cc/{id}/spend", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		acc, err := d.Store.Account(sess.Household.ID, id)
		if err != nil || acc.Purpose != "cc_buffer" {
			http.NotFound(w, r)
			return
		}
		desc := strings.TrimSpace(r.PostFormValue("description"))
		amount, err := ParseAmount(r.PostFormValue("amount"))
		if desc == "" || err != nil || amount <= 0 {
			renderAccountsBody(d, w, r, http.StatusUnprocessableEntity, "Enter a description and a positive amount.")
			return
		}
		cashback, err := ParseAmount(orZero(r.PostFormValue("cashback")))
		if err != nil {
			renderAccountsBody(d, w, r, http.StatusUnprocessableEntity, "That cashback amount doesn't look valid.")
			return
		}
		if _, err := d.Store.CreateCCTransaction(&model.CCTransaction{
			AccountID: acc.ID, Description: desc, AmountCents: amount, Currency: acc.Currency, CashbackCents: cashback,
		}); err != nil {
			http.Error(w, "could not record spend", http.StatusInternalServerError)
			return
		}
		// The discipline: spend on card → set aside in the buffer immediately.
		if err := d.Store.AdjustBalance(sess.Household.ID, acc.ID, amount); err != nil {
			http.Error(w, "could not update buffer", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(r.Context(), sess.Household.ID)
		renderAccountsBody(d, w, r, http.StatusOK, "")
	})

	mux.HandleFunc("POST /accounts/cc/{id}/bill-paid", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		acc, err := d.Store.Account(sess.Household.ID, id)
		if err != nil || acc.Purpose != "cc_buffer" {
			http.NotFound(w, r)
			return
		}
		amount, err := ParseAmount(r.PostFormValue("amount"))
		if err != nil || amount <= 0 {
			renderAccountsBody(d, w, r, http.StatusUnprocessableEntity, "Enter a positive amount.")
			return
		}
		// ponytail: cc_transactions has no archive/paid flag, so "bill paid" only
		// decrements the buffer; the spend list stays as history. Add a column if
		// per-bill reconciliation is ever needed.
		if err := d.Store.AdjustBalance(sess.Household.ID, acc.ID, -amount); err != nil {
			http.Error(w, "could not update buffer", http.StatusInternalServerError)
			return
		}
		jobs.SnapshotHousehold(r.Context(), sess.Household.ID)
		renderAccountsBody(d, w, r, http.StatusOK, "")
	})
}

// ValidPurpose reports whether p is one of the account purposes the schema
// accepts (kept in sync with the CHECK constraint in migrations).
func ValidPurpose(p string) bool {
	switch p {
	case "checking", "savings", "investment", "cc_buffer", "envelope", "pension":
		return true
	}
	return false
}

func orZero(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}

// parseOwnerID resolves the account-owner select ("", "a" or "b") to a user
// ID, or nil for a household account.
func parseOwnerID(sess *auth.Session, v string) *int64 {
	switch v {
	case "a":
		if sess.User != nil {
			id := sess.User.ID
			return &id
		}
	case "b":
		if sess.Partner != nil {
			id := sess.Partner.ID
			return &id
		}
	}
	return nil
}
