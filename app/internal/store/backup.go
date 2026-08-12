package store

import (
	"errors"
	"strings"

	"github.com/mattmezza/paripari/internal/model"
)

// BackupVersion is the shape of the export document. Bump it whenever the JSON
// changes incompatibly; the importer refuses versions it does not know.
const BackupVersion = 1

// BackupHousehold is the household row minus what isn't the user's data: the
// invite code is a credential, the id belongs to whoever is importing.
type BackupHousehold struct {
	Name                  string
	SplitMethod           string
	WeightBasis           string
	IncludeVariableIncome bool
	DisplayCurrency       string
	ManualGoldPriceCents  *int64
}

// BackupPartner names a household member so owner references in the file mean
// something. Never carries the password hash — import maps onto users that
// already exist and never creates, deletes or re-authenticates one.
type BackupPartner struct {
	ID    int64 // the exporting household's id; informational, never reused
	Name  string
	Email string
}

type BackupTransferConfirmation struct {
	Payload     string
	ConfirmedAt string
}

// Backup is one household's complete export. Every `ID`/`HouseholdID` inside is
// informational only: the importer inserts fresh rows and remaps the foreign
// keys, so a file from household A imported by B produces B's own copy.
type Backup struct {
	Version    int
	ExportedAt string

	Household BackupHousehold
	Partners  []BackupPartner

	IncomeSources      []model.IncomeSource // deductions nested
	Expenses           []model.Expense
	Accounts           []model.Account
	CCTransactions     []model.CCTransaction
	Assets             []model.Asset
	GoldItems          []model.GoldItem
	Goals              []model.Goal
	Scenarios          []model.Scenario // changes nested
	TripPlans          []model.TripPlan // items nested
	NetWorthSnapshots  []model.NetWorthSnapshot
	FinancialSnapshots []model.FinancialSnapshot
	SavedProjections   []model.SavedProjection

	TransferConfirmation *BackupTransferConfirmation `json:",omitempty"`
}

// ImportCounts reports what an import wrote, and what it could not place.
type ImportCounts struct {
	Accounts, Expenses, IncomeSources, Deductions, CCTransactions int
	Assets, GoldItems, Goals                                      int
	Scenarios, ScenarioChanges, TripPlans, TripItems              int
	NetWorthSnapshots, FinancialSnapshots, SavedProjections       int

	// SkippedNoPartner counts rows that could not be written because they
	// belong to a partner this household has no counterpart for (personal
	// expenses and income sources both require an owner).
	SkippedNoPartner int
	// AccountsUnassigned counts accounts whose owner did not map; they are
	// imported as household-wide accounts rather than dropped.
	AccountsUnassigned int
	// SkippedChanges counts scenario changes whose target row did not survive.
	SkippedChanges int
}

// ExportHousehold reads everything householdID owns. Every query is scoped to
// that household, child tables through their parent. Excluded on purpose:
// password hashes, sessions, the invite code, and the fx_rates / gold_prices
// caches (global market data, refetchable, not household-owned).
func (s *Store) ExportHousehold(householdID int64) (*Backup, error) {
	hh, err := s.Household(householdID)
	if err != nil {
		return nil, err
	}
	b := &Backup{
		Version:    BackupVersion,
		ExportedAt: now(),
		Household: BackupHousehold{
			Name: hh.Name, SplitMethod: hh.SplitMethod,
			WeightBasis:           hh.WeightBasis,
			IncludeVariableIncome: hh.IncludeVariableIncome,
			DisplayCurrency:       hh.DisplayCurrency,
			ManualGoldPriceCents:  hh.ManualGoldPriceCents,
		},
	}

	users, err := s.UsersByHousehold(householdID)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		b.Partners = append(b.Partners, BackupPartner{ID: u.ID, Name: u.Name, Email: u.Email})
	}

	if b.IncomeSources, err = s.Incomes(householdID); err != nil {
		return nil, err
	}
	if b.Expenses, err = s.Expenses(householdID); err != nil {
		return nil, err
	}
	if b.Accounts, err = s.Accounts(householdID); err != nil {
		return nil, err
	}
	for _, a := range b.Accounts {
		txs, err := s.CCTransactions(householdID, a.ID, 0)
		if err != nil {
			return nil, err
		}
		b.CCTransactions = append(b.CCTransactions, txs...)
	}
	if b.Assets, err = s.Assets(householdID); err != nil {
		return nil, err
	}
	if b.GoldItems, err = s.GoldItems(householdID); err != nil {
		return nil, err
	}
	if b.Goals, err = s.Goals(householdID); err != nil {
		return nil, err
	}
	if b.Scenarios, err = s.Scenarios(householdID); err != nil {
		return nil, err
	}
	if b.TripPlans, err = s.Trips(householdID); err != nil {
		return nil, err
	}
	if b.NetWorthSnapshots, err = s.Snapshots(householdID, 0); err != nil {
		return nil, err
	}
	if b.FinancialSnapshots, err = s.FinancialSnapshots(householdID, ""); err != nil {
		return nil, err
	}
	if b.SavedProjections, err = s.SavedProjections(householdID); err != nil {
		return nil, err
	}
	// ponytail: only the latest confirmation — it is a "these amounts were
	// agreed" marker, not a log. Export the whole table if history matters.
	if last, err := s.LastTransferConfirmation(householdID); err == nil {
		b.TransferConfirmation = &BackupTransferConfirmation{Payload: last.Payload, ConfirmedAt: last.ConfirmedAt}
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return b, nil
}

// ImportHousehold replaces everything householdID owns with the contents of b,
// in a single transaction: on any error the database is left exactly as it was.
// Ids in b are untrusted — nothing is reused as a primary or foreign key, every
// row is inserted fresh into householdID and children are remapped old -> new.
// Validate b before calling this (see handler.ValidateBackup): once the
// transaction opens, the household's rows are already gone.
func (s *Store) ImportHousehold(householdID int64, b *Backup) (ImportCounts, error) {
	var c ImportCounts
	tx, err := s.DB.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback() // no-op once Commit succeeded

	// --- partner mapping: exact email first, then position in id order ---
	rows, err := tx.Query(`SELECT id, email FROM users WHERE household_id = ? ORDER BY id`, householdID)
	if err != nil {
		return c, err
	}
	type member struct {
		id    int64
		email string
	}
	var members []member
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.id, &m.email); err != nil {
			rows.Close()
			return c, err
		}
		members = append(members, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return c, err
	}

	userMap, used := map[int64]int64{}, map[int64]bool{}
	for _, p := range b.Partners {
		for _, m := range members {
			if !used[m.id] && p.Email != "" && strings.EqualFold(m.email, p.Email) {
				userMap[p.ID], used[m.id] = m.id, true
				break
			}
		}
	}
	for _, p := range b.Partners {
		if _, ok := userMap[p.ID]; ok {
			continue
		}
		for _, m := range members {
			if !used[m.id] {
				userMap[p.ID], used[m.id] = m.id, true
				break
			}
		}
	}
	// mapUser translates an owner reference. ok is false when the file names a
	// partner this household does not have.
	mapUser := func(old *int64) (*int64, bool) {
		if old == nil {
			return nil, true
		}
		id, ok := userMap[*old]
		if !ok {
			return nil, false
		}
		return &id, true
	}

	// --- wipe: children first, then parents (cascade is belt, this is braces) ---
	for _, q := range []string{
		`DELETE FROM income_deductions WHERE income_source_id IN (SELECT id FROM income_sources WHERE household_id = ?)`,
		`DELETE FROM cc_transactions WHERE account_id IN (SELECT id FROM accounts WHERE household_id = ?)`,
		`DELETE FROM scenario_changes WHERE scenario_id IN (SELECT id FROM scenarios WHERE household_id = ?)`,
		`DELETE FROM trip_items WHERE trip_plan_id IN (SELECT id FROM trip_plans WHERE household_id = ?)`,
		`DELETE FROM expenses WHERE household_id = ?`,
		`DELETE FROM accounts WHERE household_id = ?`,
		`DELETE FROM income_sources WHERE household_id = ?`,
		`DELETE FROM assets WHERE household_id = ?`,
		`DELETE FROM gold_items WHERE household_id = ?`,
		`DELETE FROM goals WHERE household_id = ?`,
		`DELETE FROM scenarios WHERE household_id = ?`,
		`DELETE FROM trip_plans WHERE household_id = ?`,
		`DELETE FROM net_worth_snapshots WHERE household_id = ?`,
		`DELETE FROM financial_snapshots WHERE household_id = ?`,
		`DELETE FROM saved_projections WHERE household_id = ?`,
		`DELETE FROM transfer_confirmations WHERE household_id = ?`,
	} {
		if _, err := tx.Exec(q, householdID); err != nil {
			return c, err
		}
	}

	ins := func(q string, args ...any) (int64, error) {
		res, err := tx.Exec(q, args...)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	stamp := func(s string) string {
		if s == "" {
			return now()
		}
		return s
	}

	// An older file has no basis; the column is NOT NULL, so default it.
	if b.Household.WeightBasis == "" {
		b.Household.WeightBasis = "net"
	}
	if _, err := tx.Exec(`UPDATE households SET name = ?, split_method = ?, weight_basis = ?, include_variable_income = ?,
		display_currency = ?, manual_gold_price_cents = ? WHERE id = ?`,
		b.Household.Name, b.Household.SplitMethod, b.Household.WeightBasis, b.Household.IncludeVariableIncome,
		b.Household.DisplayCurrency, b.Household.ManualGoldPriceCents, householdID); err != nil {
		return c, err
	}

	accMap := map[int64]int64{}
	for _, a := range b.Accounts {
		uid, ok := mapUser(a.UserID)
		if !ok {
			c.AccountsUnassigned++ // owner not in this household: keep it, unowned
		}
		id, err := ins(`INSERT INTO accounts
			(household_id, user_id, name, institution, iban, currency, balance_cents, purpose, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			householdID, uid, a.Name, a.Institution, a.IBAN, a.Currency, a.BalanceCents, a.Purpose, stamp(a.CreatedAt))
		if err != nil {
			return c, err
		}
		accMap[a.ID] = id
		c.Accounts++
	}

	for _, t := range b.CCTransactions {
		accID, ok := accMap[t.AccountID]
		if !ok {
			continue
		}
		if _, err := ins(`INSERT INTO cc_transactions
			(account_id, description, amount_cents, currency, cashback_cents, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			accID, t.Description, t.AmountCents, t.Currency, t.CashbackCents, stamp(t.CreatedAt)); err != nil {
			return c, err
		}
		c.CCTransactions++
	}

	incMap := map[int64]int64{}
	for _, in := range b.IncomeSources {
		uid, ok := mapUser(&in.UserID)
		if !ok || uid == nil {
			c.SkippedNoPartner++ // income_sources.user_id is NOT NULL: no owner, no row
			continue
		}
		id, err := ins(`INSERT INTO income_sources
			(household_id, user_id, name, kind, pay_structure, gross_yearly_cents, currency, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			householdID, *uid, in.Name, in.Kind, in.PayStructure, in.GrossYearlyCents, in.Currency, stamp(in.CreatedAt))
		if err != nil {
			return c, err
		}
		incMap[in.ID] = id
		c.IncomeSources++
		for _, d := range in.Deductions {
			if _, err := ins(`INSERT INTO income_deductions
				(income_source_id, name, amount_cents, period, percent_bp) VALUES (?, ?, ?, ?, ?)`,
				id, d.Name, d.AmountCents, d.Period, d.PercentBP); err != nil {
				return c, err
			}
			c.Deductions++
		}
	}

	expMap := map[int64]int64{}
	for _, e := range b.Expenses {
		uid, ok := mapUser(e.UserID)
		if !ok {
			c.SkippedNoPartner++ // a personal expense with no owner breaks the CHECK
			continue
		}
		var accID *int64
		if e.AccountID != nil {
			if id, ok := accMap[*e.AccountID]; ok {
				accID = &id
			}
		}
		id, err := ins(`INSERT INTO expenses
			(household_id, name, amount_cents, currency, category, user_id, subcategory, kind, account_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			householdID, e.Name, e.AmountCents, e.Currency, e.Category, uid, e.Subcategory,
			kindOrDefault(e.Kind), accID, stamp(e.CreatedAt))
		if err != nil {
			return c, err
		}
		expMap[e.ID] = id
		c.Expenses++
	}

	assetMap := map[int64]int64{}
	for _, a := range b.Assets {
		id, err := ins(`INSERT INTO assets (household_id, name, kind, estimated_value_cents, currency, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			householdID, a.Name, a.Kind, a.EstimatedValueCents, a.Currency, stamp(a.CreatedAt))
		if err != nil {
			return c, err
		}
		assetMap[a.ID] = id
		c.Assets++
	}

	for _, g := range b.GoldItems {
		if _, err := ins(`INSERT INTO gold_items
			(household_id, name, weight_grams, purity_karat, quantity, location, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			householdID, g.Name, g.WeightGrams, g.PurityKarat, g.Quantity, g.Location, stamp(g.CreatedAt)); err != nil {
			return c, err
		}
		c.GoldItems++
	}

	for _, g := range b.Goals {
		if _, err := ins(`INSERT INTO goals
			(household_id, name, target_amount_cents, currency, deadline, category, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			householdID, g.Name, g.TargetAmountCents, g.Currency, g.Deadline, g.Category, stamp(g.CreatedAt)); err != nil {
			return c, err
		}
		c.Goals++
	}

	for _, sc := range b.Scenarios {
		sid, err := ins(`INSERT INTO scenarios (household_id, name, description, created_at) VALUES (?, ?, ?, ?)`,
			householdID, sc.Name, sc.Description, stamp(sc.CreatedAt))
		if err != nil {
			return c, err
		}
		c.Scenarios++
		for _, ch := range sc.Changes {
			target, ok := remapTarget(ch, expMap, incMap, assetMap, userMap)
			if !ok {
				c.SkippedChanges++ // the row this change pointed at did not survive
				continue
			}
			if _, err := ins(`INSERT INTO scenario_changes
				(scenario_id, change_type, target_id, label, value_cents, value_num, value_text, currency)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				sid, ch.ChangeType, target, ch.Label, ch.ValueCents, ch.ValueNum, ch.ValueText, ch.Currency); err != nil {
				return c, err
			}
			c.ScenarioChanges++
		}
	}

	for _, t := range b.TripPlans {
		tid, err := ins(`INSERT INTO trip_plans (household_id, name, start_date, months_to_save, committed, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			householdID, t.Name, t.StartDate, t.MonthsToSave, t.Committed, stamp(t.CreatedAt))
		if err != nil {
			return c, err
		}
		c.TripPlans++
		for _, it := range t.Items {
			if _, err := ins(`INSERT INTO trip_items (trip_plan_id, name, category, amount_cents, currency)
				VALUES (?, ?, ?, ?, ?)`, tid, it.Name, it.Category, it.AmountCents, it.Currency); err != nil {
				return c, err
			}
			c.TripItems++
		}
	}

	for _, sn := range b.NetWorthSnapshots {
		if _, err := ins(`INSERT INTO net_worth_snapshots
			(household_id, date, liquid_cents, alternative_cents, real_estate_cents) VALUES (?, ?, ?, ?, ?)`,
			householdID, sn.Date, sn.LiquidCents, sn.AlternativeCents, sn.RealEstateCents); err != nil {
			return c, err
		}
		c.NetWorthSnapshots++
	}

	for _, sn := range b.FinancialSnapshots {
		if _, err := ins(`INSERT INTO financial_snapshots
			(household_id, date, currency, income_cents, expenses_cents, savings_cents,
			 available_cents, common_expenses_cents, personal_expenses_cents)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			householdID, sn.Date, sn.Currency, sn.IncomeCents, sn.ExpensesCents, sn.SavingsCents,
			sn.AvailableCents, sn.CommonExpensesCents, sn.PersonalExpensesCents); err != nil {
			return c, err
		}
		c.FinancialSnapshots++
	}

	for _, sp := range b.SavedProjections {
		if _, err := ins(`INSERT INTO saved_projections (household_id, name, params, created_at) VALUES (?, ?, ?, ?)`,
			householdID, sp.Name, sp.Params, stamp(sp.CreatedAt)); err != nil {
			return c, err
		}
		c.SavedProjections++
	}

	if tc := b.TransferConfirmation; tc != nil {
		if _, err := ins(`INSERT INTO transfer_confirmations (household_id, payload, confirmed_at) VALUES (?, ?, ?)`,
			householdID, tc.Payload, stamp(tc.ConfirmedAt)); err != nil {
			return c, err
		}
	}

	return c, tx.Commit()
}

// remapTarget translates a scenario change's target_id, whose meaning depends
// on change_type (see service/scenario.go). ok is false when the target row no
// longer exists, in which case the change is dropped rather than left pointing
// at a stranger's id.
func remapTarget(ch model.ScenarioChange, exp, inc, asset, user map[int64]int64) (*int64, bool) {
	var table map[int64]int64
	switch ch.ChangeType {
	case "expense_amount", "expense_remove":
		table = exp
	case "income_amount", "income_remove":
		table = inc
	case "asset_remove":
		table = asset
	case "expense_add", "income_add":
		table = user // target is the owning partner (nil = common expense)
	default:
		return nil, true // return_rate, asset_add: no target
	}
	if ch.TargetID == nil {
		// income_add needs an owner; the others tolerate "no target".
		return nil, ch.ChangeType != "income_add"
	}
	id, ok := table[*ch.TargetID]
	if !ok {
		return nil, false
	}
	return &id, true
}
