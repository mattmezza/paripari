package store

import "github.com/mattmezza/paripari/internal/model"

// CreateCCTransaction records a credit-card spend against a buffer account.
func (s *Store) CreateCCTransaction(t *model.CCTransaction) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO cc_transactions
		(account_id, description, amount_cents, currency, cashback_cents, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.AccountID, t.Description, t.AmountCents, t.Currency, t.CashbackCents, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteCCTransaction(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM cc_transactions WHERE id = ?
		AND account_id IN (SELECT id FROM accounts WHERE household_id = ?)`, id, householdID)
	return err
}

// CCTransactions returns the most recent transactions for an account (limit <= 0 means all).
func (s *Store) CCTransactions(accountID int64, limit int) ([]model.CCTransaction, error) {
	q := `SELECT id, account_id, description, amount_cents, currency, cashback_cents, created_at
		FROM cc_transactions WHERE account_id = ? ORDER BY created_at DESC, id DESC`
	args := []any{accountID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CCTransaction
	for rows.Next() {
		var t model.CCTransaction
		if err := rows.Scan(&t.ID, &t.AccountID, &t.Description, &t.AmountCents, &t.Currency,
			&t.CashbackCents, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CCCashbackTotal sums cashback earned on an account.
func (s *Store) CCCashbackTotal(accountID int64) (int64, error) {
	var v int64
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(cashback_cents),0) FROM cc_transactions WHERE account_id = ?`, accountID).Scan(&v)
	return v, err
}
