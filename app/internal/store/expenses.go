package store

import "github.com/mattmezza/paripari/internal/model"

const expenseCols = `id, household_id, name, amount_cents, currency, category, user_id, subcategory, is_savings, account_id, created_at`

func scanExpense(sc interface{ Scan(...any) error }) (model.Expense, error) {
	var e model.Expense
	err := sc.Scan(&e.ID, &e.HouseholdID, &e.Name, &e.AmountCents, &e.Currency, &e.Category,
		&e.UserID, &e.Subcategory, &e.IsSavings, &e.AccountID, &e.CreatedAt)
	return e, err
}

func (s *Store) CreateExpense(e *model.Expense) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO expenses
		(household_id, name, amount_cents, currency, category, user_id, subcategory, is_savings, account_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.HouseholdID, e.Name, e.AmountCents, e.Currency, e.Category, e.UserID, e.Subcategory, e.IsSavings, e.AccountID, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateExpense(e *model.Expense) error {
	_, err := s.DB.Exec(`UPDATE expenses SET name = ?, amount_cents = ?, currency = ?, category = ?,
		user_id = ?, subcategory = ?, is_savings = ?, account_id = ? WHERE id = ? AND household_id = ?`,
		e.Name, e.AmountCents, e.Currency, e.Category, e.UserID, e.Subcategory, e.IsSavings, e.AccountID, e.ID, e.HouseholdID)
	return err
}

func (s *Store) DeleteExpense(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM expenses WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) Expense(householdID, id int64) (*model.Expense, error) {
	e, err := scanExpense(s.DB.QueryRow(`SELECT `+expenseCols+` FROM expenses WHERE id = ? AND household_id = ?`, id, householdID))
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &e, nil
}

func (s *Store) Expenses(householdID int64) ([]model.Expense, error) {
	// Insertion order within each category, so a new expense appends to the
	// bottom of its group instead of sorting itself into the middle (or, when
	// it has no subcategory, to the very top of the page).
	rows, err := s.DB.Query(`SELECT `+expenseCols+` FROM expenses WHERE household_id = ?
		ORDER BY category, id`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Expense
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
