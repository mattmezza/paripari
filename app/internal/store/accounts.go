package store

import "github.com/mattmezza/paripari/internal/model"

const accountCols = `id, household_id, user_id, name, institution, iban, currency, balance_cents, purpose, created_at`

func scanAccount(sc interface{ Scan(...any) error }) (model.Account, error) {
	var a model.Account
	err := sc.Scan(&a.ID, &a.HouseholdID, &a.UserID, &a.Name, &a.Institution, &a.IBAN,
		&a.Currency, &a.BalanceCents, &a.Purpose, &a.CreatedAt)
	return a, err
}

func (s *Store) CreateAccount(a *model.Account) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO accounts
		(household_id, user_id, name, institution, iban, currency, balance_cents, purpose, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.HouseholdID, a.UserID, a.Name, a.Institution, a.IBAN, a.Currency, a.BalanceCents, a.Purpose, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateAccount(a *model.Account) error {
	_, err := s.DB.Exec(`UPDATE accounts SET user_id = ?, name = ?, institution = ?, iban = ?, currency = ?,
		balance_cents = ?, purpose = ? WHERE id = ? AND household_id = ?`,
		a.UserID, a.Name, a.Institution, a.IBAN, a.Currency, a.BalanceCents, a.Purpose, a.ID, a.HouseholdID)
	return err
}

func (s *Store) DeleteAccount(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM accounts WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) Account(householdID, id int64) (*model.Account, error) {
	a, err := scanAccount(s.DB.QueryRow(`SELECT `+accountCols+` FROM accounts WHERE id = ? AND household_id = ?`, id, householdID))
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &a, nil
}

func (s *Store) Accounts(householdID int64) ([]model.Account, error) {
	rows, err := s.DB.Query(`SELECT `+accountCols+` FROM accounts WHERE household_id = ? ORDER BY purpose, id`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AdjustBalance adds delta cents to an account balance.
func (s *Store) AdjustBalance(householdID, id, deltaCents int64) error {
	_, err := s.DB.Exec(`UPDATE accounts SET balance_cents = balance_cents + ? WHERE id = ? AND household_id = ?`,
		deltaCents, id, householdID)
	return err
}
