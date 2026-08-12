package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
)

// maxImportBytes caps the uploaded file. A household's entire history — every
// expense, account and snapshot it ever had — serialises to tens of kilobytes,
// so 8 MiB is orders of magnitude of headroom while keeping a hostile upload
// from exhausting memory.
const maxImportBytes = 8 << 20

// registerBackup adds export/import to the settings page. partial is
// registerSettings' section renderer.
func registerBackup(mux *http.ServeMux, d *Deps,
	partial func(w http.ResponseWriter, r *http.Request, name, notice, errMsg string, status int)) {

	mux.HandleFunc("GET /settings/export", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		b, err := d.Store.ExportHousehold(sess.Household.ID)
		if err != nil {
			http.Error(w, "could not export", http.StatusInternalServerError)
			return
		}
		// Indented: the file is meant to be readable (and diffable) by hand.
		body, err := json.MarshalIndent(b, "", "  ")
		if err != nil {
			http.Error(w, "could not export", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition",
			`attachment; filename="paripari-export-`+time.Now().UTC().Format("2006-01-02")+`.json"`)
		w.Write(body)
	})

	mux.HandleFunc("POST /settings/import", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		fail := func(msg string) {
			partial(w, r, "settings-backup", "", msg, http.StatusUnprocessableEntity)
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			fail("That upload could not be read — is it a file no larger than 8 MB?")
			return
		}
		// Never trust the client's disabled button: no confirmation, no import.
		if r.PostFormValue("confirm") != "on" {
			fail("Tick the confirmation box first — importing replaces everything in this household.")
			return
		}
		file, fh, err := r.FormFile("file")
		if err != nil {
			fail("Pick a backup file to import.")
			return
		}
		defer file.Close()
		if fh.Size > maxImportBytes {
			fail("That file is larger than the 8 MB limit.")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(file, maxImportBytes+1))
		if err != nil || len(raw) > maxImportBytes {
			fail("That file could not be read, or is larger than the 8 MB limit.")
			return
		}

		// Parse and validate everything before touching the database: once the
		// transaction opens, the household's current rows are gone.
		var b store.Backup
		if err := json.Unmarshal(raw, &b); err != nil {
			fail("That file isn't valid JSON: " + err.Error())
			return
		}
		if err := ValidateBackup(&b); err != nil {
			fail("That backup can't be imported: " + err.Error())
			return
		}

		counts, err := d.Store.ImportHousehold(sess.Household.ID, &b)
		if err != nil {
			fail("The import failed and nothing was changed: " + err.Error())
			return
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		partial(w, r, "settings-backup", importSummary(counts), "", http.StatusOK)
	})
}

func importSummary(c store.ImportCounts) string {
	var parts []string
	add := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	add(c.IncomeSources, "income source", "income sources")
	add(c.Deductions, "deduction", "deductions")
	add(c.Expenses, "expense", "expenses")
	add(c.Accounts, "account", "accounts")
	add(c.CCTransactions, "card transaction", "card transactions")
	add(c.Assets, "asset", "assets")
	add(c.GoldItems, "gold item", "gold items")
	add(c.Goals, "goal", "goals")
	add(c.Scenarios, "scenario", "scenarios")
	add(c.ScenarioChanges, "scenario change", "scenario changes")
	add(c.TripPlans, "trip", "trips")
	add(c.TripItems, "trip item", "trip items")
	add(c.NetWorthSnapshots, "net worth snapshot", "net worth snapshots")
	add(c.FinancialSnapshots, "history snapshot", "history snapshots")

	out := "Imported: nothing (the file was empty)."
	if len(parts) > 0 {
		out = "Imported " + strings.Join(parts, ", ") + "."
	}
	if c.SkippedNoPartner > 0 {
		out += fmt.Sprintf(" %d row(s) belonged to a partner this household doesn't have and were not imported.",
			c.SkippedNoPartner)
	}
	if c.AccountsUnassigned > 0 {
		out += fmt.Sprintf(" %d account(s) lost their owner and are now household-wide.", c.AccountsUnassigned)
	}
	if c.SkippedChanges > 0 {
		out += fmt.Sprintf(" %d scenario change(s) pointed at a row that isn't in the file and were dropped.",
			c.SkippedChanges)
	}
	return out
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// ValidateBackup checks a parsed export end to end — version, enums, currencies,
// required fields and whether the file's own foreign keys resolve — so a
// truncated or hand-edited file is refused before anything is deleted.
func ValidateBackup(b *store.Backup) error {
	if b.Version != store.BackupVersion {
		return fmt.Errorf("this file says version %d, this version of PariPari reads version %d",
			b.Version, store.BackupVersion)
	}
	h := b.Household
	if strings.TrimSpace(h.Name) == "" {
		return fmt.Errorf("household name is missing")
	}
	if !oneOf(h.SplitMethod, "fifty_fifty", "income_weighted") {
		return fmt.Errorf("household split method %q is not valid", h.SplitMethod)
	}
	// Files written before the gross basis existed carry no value at all.
	if h.WeightBasis != "" && !oneOf(h.WeightBasis, "net", "gross") {
		return fmt.Errorf("household weight basis %q is not valid", h.WeightBasis)
	}
	if !ValidCurrency(h.DisplayCurrency) {
		return fmt.Errorf("household display currency %q is not supported", h.DisplayCurrency)
	}

	for i, in := range b.IncomeSources {
		where := fmt.Sprintf("income source #%d (%q)", i+1, in.Name)
		if strings.TrimSpace(in.Name) == "" {
			return fmt.Errorf("%s has no name", where)
		}
		if !oneOf(in.Kind, "fixed", "variable") {
			return fmt.Errorf("%s has kind %q", where, in.Kind)
		}
		if in.PayStructure != 12 && in.PayStructure != 13 {
			return fmt.Errorf("%s has pay structure %d, expected 12 or 13", where, in.PayStructure)
		}
		if in.GrossYearlyCents < 0 {
			return fmt.Errorf("%s has a negative gross", where)
		}
		if !ValidCurrency(in.Currency) {
			return fmt.Errorf("%s has currency %q", where, in.Currency)
		}
		for j, d := range in.Deductions {
			dw := fmt.Sprintf("%s, deduction #%d (%q)", where, j+1, d.Name)
			if strings.TrimSpace(d.Name) == "" {
				return fmt.Errorf("%s has no name", dw)
			}
			if !oneOf(d.Period, "monthly", "yearly", "percent") {
				return fmt.Errorf("%s has period %q", dw, d.Period)
			}
			if d.AmountCents < 0 || d.PercentBP < 0 {
				return fmt.Errorf("%s has a negative amount", dw)
			}
		}
	}

	accounts := map[int64]bool{}
	for i, a := range b.Accounts {
		where := fmt.Sprintf("account #%d (%q)", i+1, a.Name)
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("%s has no name", where)
		}
		if !oneOf(a.Purpose, "checking", "savings", "investment", "cc_buffer", "envelope", "pension") {
			return fmt.Errorf("%s has purpose %q", where, a.Purpose)
		}
		if !ValidCurrency(a.Currency) {
			return fmt.Errorf("%s has currency %q", where, a.Currency)
		}
		accounts[a.ID] = true
	}

	for i, e := range b.Expenses {
		where := fmt.Sprintf("expense #%d (%q)", i+1, e.Name)
		if strings.TrimSpace(e.Name) == "" {
			return fmt.Errorf("%s has no name", where)
		}
		if !oneOf(e.Category, "personal", "common") {
			return fmt.Errorf("%s has category %q", where, e.Category)
		}
		if e.Category == "personal" && e.UserID == nil {
			return fmt.Errorf("%s is personal but names no partner", where)
		}
		if e.Category == "common" && e.UserID != nil {
			return fmt.Errorf("%s is common but names a partner", where)
		}
		if e.AmountCents < 0 {
			return fmt.Errorf("%s has a negative amount", where)
		}
		if !ValidCurrency(e.Currency) {
			return fmt.Errorf("%s has currency %q", where, e.Currency)
		}
		if e.AccountID != nil && !accounts[*e.AccountID] {
			return fmt.Errorf("%s points at an account that isn't in this file", where)
		}
	}

	for i, t := range b.CCTransactions {
		where := fmt.Sprintf("card transaction #%d (%q)", i+1, t.Description)
		if !accounts[t.AccountID] {
			return fmt.Errorf("%s points at an account that isn't in this file", where)
		}
		if !ValidCurrency(t.Currency) {
			return fmt.Errorf("%s has currency %q", where, t.Currency)
		}
	}

	for i, a := range b.Assets {
		where := fmt.Sprintf("asset #%d (%q)", i+1, a.Name)
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("%s has no name", where)
		}
		if !oneOf(a.Kind, "real_estate", "other") {
			return fmt.Errorf("%s has kind %q", where, a.Kind)
		}
		if !ValidCurrency(a.Currency) {
			return fmt.Errorf("%s has currency %q", where, a.Currency)
		}
	}

	for i, g := range b.GoldItems {
		where := fmt.Sprintf("gold item #%d (%q)", i+1, g.Name)
		if strings.TrimSpace(g.Name) == "" {
			return fmt.Errorf("%s has no name", where)
		}
		if g.WeightGrams <= 0 {
			return fmt.Errorf("%s has weight %v", where, g.WeightGrams)
		}
		if g.PurityKarat < 1 || g.PurityKarat > 24 {
			return fmt.Errorf("%s has purity %d karat", where, g.PurityKarat)
		}
		if g.Quantity < 1 {
			return fmt.Errorf("%s has quantity %d", where, g.Quantity)
		}
	}

	for i, g := range b.Goals {
		where := fmt.Sprintf("goal #%d (%q)", i+1, g.Name)
		if strings.TrimSpace(g.Name) == "" {
			return fmt.Errorf("%s has no name", where)
		}
		if g.TargetAmountCents <= 0 {
			return fmt.Errorf("%s has a target of %d cents", where, g.TargetAmountCents)
		}
		if !ValidCurrency(g.Currency) {
			return fmt.Errorf("%s has currency %q", where, g.Currency)
		}
	}

	changeTypes := []string{"expense_amount", "expense_add", "expense_remove", "income_amount",
		"income_add", "income_remove", "return_rate", "asset_add", "asset_remove"}
	for i, sc := range b.Scenarios {
		where := fmt.Sprintf("scenario #%d (%q)", i+1, sc.Name)
		if strings.TrimSpace(sc.Name) == "" {
			return fmt.Errorf("%s has no name", where)
		}
		for j, ch := range sc.Changes {
			if !oneOf(ch.ChangeType, changeTypes...) {
				return fmt.Errorf("%s, change #%d has type %q", where, j+1, ch.ChangeType)
			}
		}
	}

	for i, t := range b.TripPlans {
		where := fmt.Sprintf("trip #%d (%q)", i+1, t.Name)
		if strings.TrimSpace(t.Name) == "" {
			return fmt.Errorf("%s has no name", where)
		}
		if t.MonthsToSave < 1 {
			return fmt.Errorf("%s saves over %d months", where, t.MonthsToSave)
		}
		// "" is how a file written before funding strategies existed reads, and
		// the importer defaults it to spread.
		if t.FundingStrategy != "" && !model.ValidTripStrategy(t.FundingStrategy) {
			return fmt.Errorf("%s has funding strategy %q", where, t.FundingStrategy)
		}
		for j, it := range t.Items {
			iw := fmt.Sprintf("%s, item #%d (%q)", where, j+1, it.Name)
			if strings.TrimSpace(it.Name) == "" {
				return fmt.Errorf("%s has no name", iw)
			}
			if it.AmountCents < 0 {
				return fmt.Errorf("%s has a negative amount", iw)
			}
			if !ValidCurrency(it.Currency) {
				return fmt.Errorf("%s has currency %q", iw, it.Currency)
			}
		}
	}

	for i, p := range b.SavedProjections {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("saved projection #%d has no name", i+1)
		}
		// Params is deliberately not validated beyond its size: it is a query
		// string the projections page re-parses, and every knob there already
		// falls back to its default when the value is missing or junk.
		if len(p.Params) > maxSavedParams {
			return fmt.Errorf("saved projection #%d (%q) carries %d characters of settings, more than the %d allowed",
				i+1, p.Name, len(p.Params), maxSavedParams)
		}
	}

	for i, sn := range b.NetWorthSnapshots {
		if !validDate(sn.Date) {
			return fmt.Errorf("net worth snapshot #%d has date %q, expected YYYY-MM-DD", i+1, sn.Date)
		}
	}
	for i, sn := range b.FinancialSnapshots {
		if !validDate(sn.Date) {
			return fmt.Errorf("history snapshot #%d has date %q, expected YYYY-MM-DD", i+1, sn.Date)
		}
		if !ValidCurrency(sn.Currency) {
			return fmt.Errorf("history snapshot #%d has currency %q", i+1, sn.Currency)
		}
	}
	return nil
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}
