package store

import "github.com/mattmezza/paripari/internal/model"

const tripCols = `id, household_id, name, start_date, months_to_save, committed,
	funding_account_id, funding_strategy, linked_expense_id, created_at`

// scanTrip keeps the two readers of tripCols in step.
func scanTrip(sc interface{ Scan(...any) error }, t *model.TripPlan) error {
	return sc.Scan(&t.ID, &t.HouseholdID, &t.Name, &t.StartDate, &t.MonthsToSave, &t.Committed,
		&t.FundingAccountID, &t.FundingStrategy, &t.LinkedExpenseID, &t.CreatedAt)
}

// tripAccountOwned scopes funding_account_id to the household: a foreign
// account id is written as NULL (the household default) rather than stored.
// Handlers reject it before getting here; this is the belt to that braces.
const tripAccountOwned = `(SELECT id FROM accounts WHERE id = ? AND household_id = ?)`

func (s *Store) CreateTrip(t *model.TripPlan) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO trip_plans
		(household_id, name, start_date, months_to_save, committed, funding_account_id, funding_strategy, linked_expense_id, created_at)
		VALUES (?, ?, ?, ?, ?, `+tripAccountOwned+`, ?, ?, ?)`,
		t.HouseholdID, t.Name, t.StartDate, t.MonthsToSave, t.Committed,
		t.FundingAccountID, t.HouseholdID, strategyOrDefault(t.FundingStrategy), t.LinkedExpenseID, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTrip(t *model.TripPlan) error {
	_, err := s.DB.Exec(`UPDATE trip_plans SET name = ?, start_date = ?, months_to_save = ?, committed = ?,
		funding_account_id = `+tripAccountOwned+`, funding_strategy = ?, linked_expense_id = ?
		WHERE id = ? AND household_id = ?`,
		t.Name, t.StartDate, t.MonthsToSave, t.Committed,
		t.FundingAccountID, t.HouseholdID, strategyOrDefault(t.FundingStrategy), t.LinkedExpenseID,
		t.ID, t.HouseholdID)
	return err
}

// strategyOrDefault keeps the NOT NULL column honest for zero-valued structs.
func strategyOrDefault(s string) string {
	if model.ValidTripStrategy(s) {
		return s
	}
	return model.TripSpread
}

func (s *Store) DeleteTrip(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM trip_plans WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) Trip(householdID, id int64) (*model.TripPlan, error) {
	var t model.TripPlan
	err := scanTrip(s.DB.QueryRow(`SELECT `+tripCols+` FROM trip_plans WHERE id = ? AND household_id = ?`, id, householdID), &t)
	if err != nil {
		return nil, mapNoRows(err)
	}
	t.Items, err = s.TripItems(householdID, t.ID)
	return &t, err
}

// Trips returns all trip plans of a household with their items loaded.
func (s *Store) Trips(householdID int64) ([]model.TripPlan, error) {
	rows, err := s.DB.Query(`SELECT `+tripCols+` FROM trip_plans WHERE household_id = ? ORDER BY start_date IS NULL, start_date, name`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TripPlan
	for rows.Next() {
		var t model.TripPlan
		if err := scanTrip(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Items, err = s.TripItems(householdID, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ownedTrip is the tenant predicate for trip_items: the child table has no
// household_id, so scope it through its parent trip plan.
const ownedTrip = ` AND trip_plan_id IN (SELECT id FROM trip_plans WHERE household_id = ?)`

func (s *Store) TripItems(householdID, tripID int64) ([]model.TripItem, error) {
	rows, err := s.DB.Query(`SELECT id, trip_plan_id, name, category, amount_cents, currency
		FROM trip_items WHERE trip_plan_id = ?`+ownedTrip+` ORDER BY id`, tripID, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TripItem
	for rows.Next() {
		var it model.TripItem
		if err := rows.Scan(&it.ID, &it.TripPlanID, &it.Name, &it.Category, &it.AmountCents, &it.Currency); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) CreateTripItem(householdID int64, it *model.TripItem) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO trip_items (trip_plan_id, name, category, amount_cents, currency)
		SELECT ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM trip_plans WHERE id = ? AND household_id = ?)`,
		it.TripPlanID, it.Name, it.Category, it.AmountCents, it.Currency, it.TripPlanID, householdID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTripItem(householdID int64, it *model.TripItem) error {
	_, err := s.DB.Exec(`UPDATE trip_items SET name = ?, category = ?, amount_cents = ?, currency = ?
		WHERE id = ?`+ownedTrip, it.Name, it.Category, it.AmountCents, it.Currency, it.ID, householdID)
	return err
}

func (s *Store) DeleteTripItem(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM trip_items WHERE id = ?`+ownedTrip, id, householdID)
	return err
}
