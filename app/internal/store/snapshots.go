package store

import "github.com/mattmezza/paripari/internal/model"

// PutSnapshot upserts today's (or the given date's) net worth snapshot. All amounts in CHF cents.
func (s *Store) PutSnapshot(sn *model.NetWorthSnapshot) error {
	date := sn.Date
	if date == "" {
		date = today()
	}
	_, err := s.DB.Exec(`INSERT INTO net_worth_snapshots
		(household_id, date, liquid_cents, alternative_cents, real_estate_cents) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(household_id, date) DO UPDATE SET
			liquid_cents = excluded.liquid_cents,
			alternative_cents = excluded.alternative_cents,
			real_estate_cents = excluded.real_estate_cents`,
		sn.HouseholdID, date, sn.LiquidCents, sn.AlternativeCents, sn.RealEstateCents)
	return err
}

// Snapshots returns snapshots oldest-first (limit <= 0 means all).
func (s *Store) Snapshots(householdID int64, limit int) ([]model.NetWorthSnapshot, error) {
	q := `SELECT id, household_id, date, liquid_cents, alternative_cents, real_estate_cents
		FROM net_worth_snapshots WHERE household_id = ? ORDER BY date`
	args := []any{householdID}
	if limit > 0 {
		q = `SELECT * FROM (` + q + ` DESC LIMIT ?) ORDER BY date`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.NetWorthSnapshot
	for rows.Next() {
		var sn model.NetWorthSnapshot
		if err := rows.Scan(&sn.ID, &sn.HouseholdID, &sn.Date, &sn.LiquidCents,
			&sn.AlternativeCents, &sn.RealEstateCents); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *Store) LatestSnapshot(householdID int64) (*model.NetWorthSnapshot, error) {
	var sn model.NetWorthSnapshot
	err := s.DB.QueryRow(`SELECT id, household_id, date, liquid_cents, alternative_cents, real_estate_cents
		FROM net_worth_snapshots WHERE household_id = ? ORDER BY date DESC LIMIT 1`, householdID).
		Scan(&sn.ID, &sn.HouseholdID, &sn.Date, &sn.LiquidCents, &sn.AlternativeCents, &sn.RealEstateCents)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &sn, nil
}

const finSnapCols = `id, household_id, date, currency, income_cents, expenses_cents,
	savings_cents, available_cents, common_expenses_cents, personal_expenses_cents`

// PutFinancialSnapshot upserts the monthly-picture snapshot for a date (today
// when Date is empty). All amounts in CHF cents.
func (s *Store) PutFinancialSnapshot(sn *model.FinancialSnapshot) error {
	date := sn.Date
	if date == "" {
		date = today()
	}
	cur := sn.Currency
	if cur == "" {
		cur = "CHF"
	}
	_, err := s.DB.Exec(`INSERT INTO financial_snapshots
		(household_id, date, currency, income_cents, expenses_cents, savings_cents,
		 available_cents, common_expenses_cents, personal_expenses_cents)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(household_id, date) DO UPDATE SET
			currency = excluded.currency,
			income_cents = excluded.income_cents,
			expenses_cents = excluded.expenses_cents,
			savings_cents = excluded.savings_cents,
			available_cents = excluded.available_cents,
			common_expenses_cents = excluded.common_expenses_cents,
			personal_expenses_cents = excluded.personal_expenses_cents`,
		sn.HouseholdID, date, cur, sn.IncomeCents, sn.ExpensesCents, sn.SavingsCents,
		sn.AvailableCents, sn.CommonExpensesCents, sn.PersonalExpensesCents)
	return err
}

// FinancialSnapshots returns snapshots oldest-first, from `since` (inclusive,
// YYYY-MM-DD) onwards. An empty `since` returns everything.
func (s *Store) FinancialSnapshots(householdID int64, since string) ([]model.FinancialSnapshot, error) {
	rows, err := s.DB.Query(`SELECT `+finSnapCols+` FROM financial_snapshots
		WHERE household_id = ? AND date >= ? ORDER BY date`, householdID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FinancialSnapshot
	for rows.Next() {
		var sn model.FinancialSnapshot
		if err := rows.Scan(&sn.ID, &sn.HouseholdID, &sn.Date, &sn.Currency, &sn.IncomeCents,
			&sn.ExpensesCents, &sn.SavingsCents, &sn.AvailableCents,
			&sn.CommonExpensesCents, &sn.PersonalExpensesCents); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// SnapshotsSince returns net worth snapshots oldest-first from `since`
// (inclusive). An empty `since` returns everything.
func (s *Store) SnapshotsSince(householdID int64, since string) ([]model.NetWorthSnapshot, error) {
	rows, err := s.DB.Query(`SELECT id, household_id, date, liquid_cents, alternative_cents, real_estate_cents
		FROM net_worth_snapshots WHERE household_id = ? AND date >= ? ORDER BY date`, householdID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.NetWorthSnapshot
	for rows.Next() {
		var sn model.NetWorthSnapshot
		if err := rows.Scan(&sn.ID, &sn.HouseholdID, &sn.Date, &sn.LiquidCents,
			&sn.AlternativeCents, &sn.RealEstateCents); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}
