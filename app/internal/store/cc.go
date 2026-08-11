package store

import "github.com/mattmezza/paripari/internal/model"

// ownedAccount is the tenant predicate for cc_transactions: the child table
// has no household_id, so it is scoped through the account it belongs to.
const ownedAccount = ` AND account_id IN (SELECT id FROM accounts WHERE household_id = ?)`

// CreateCCTransaction records a credit-card spend against a buffer account.
// The INSERT ... SELECT writes nothing when the account is not the
// household's, so a foreign account id can never gain a row.
func (s *Store) CreateCCTransaction(householdID int64, t *model.CCTransaction) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO cc_transactions
		(account_id, description, amount_cents, currency, cashback_cents, created_at)
		SELECT ?, ?, ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM accounts WHERE id = ? AND household_id = ?)`,
		t.AccountID, t.Description, t.AmountCents, t.Currency, t.CashbackCents, now(),
		t.AccountID, householdID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	return res.LastInsertId()
}

func (s *Store) DeleteCCTransaction(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM cc_transactions WHERE id = ?`+ownedAccount, id, householdID)
	return err
}

// CCTransactions returns the most recent transactions for an account (limit <= 0 means all).
func (s *Store) CCTransactions(householdID, accountID int64, limit int) ([]model.CCTransaction, error) {
	q := `SELECT id, account_id, description, amount_cents, currency, cashback_cents, created_at
		FROM cc_transactions WHERE account_id = ?` + ownedAccount + ` ORDER BY created_at DESC, id DESC`
	args := []any{accountID, householdID}
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
func (s *Store) CCCashbackTotal(householdID, accountID int64) (int64, error) {
	var v int64
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(cashback_cents),0) FROM cc_transactions
		WHERE account_id = ?`+ownedAccount, accountID, householdID).Scan(&v)
	return v, err
}
