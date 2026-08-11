package store

import (
	"crypto/rand"
	"database/sql"
	"errors"

	"github.com/mattmezza/paripari/internal/model"
)

var ErrNotFound = errors.New("not found")

const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous chars

// NewInviteCode returns an 8-char uppercase invite code.
func NewInviteCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	for i := range b {
		b[i] = inviteAlphabet[int(b[i])%len(inviteAlphabet)]
	}
	return string(b)
}

func (s *Store) CreateHousehold(name string) (*model.Household, error) {
	code := NewInviteCode()
	res, err := s.DB.Exec(`INSERT INTO households (name, invite_code, created_at) VALUES (?, ?, ?)`,
		name, code, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Household(id)
}

const householdCols = `id, name, split_method, include_variable_income, display_currency,
	manual_gold_price_cents, invite_code, created_at`

func scanHousehold(row *sql.Row) (*model.Household, error) {
	var h model.Household
	err := row.Scan(&h.ID, &h.Name, &h.SplitMethod, &h.IncludeVariableIncome, &h.DisplayCurrency,
		&h.ManualGoldPriceCents, &h.InviteCode, &h.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &h, err
}

func (s *Store) Household(id int64) (*model.Household, error) {
	return scanHousehold(s.DB.QueryRow(`SELECT `+householdCols+` FROM households WHERE id = ?`, id))
}

func (s *Store) HouseholdByInvite(code string) (*model.Household, error) {
	return scanHousehold(s.DB.QueryRow(`SELECT `+householdCols+` FROM households WHERE invite_code = ?`, code))
}

func (s *Store) HouseholdIDs() ([]int64, error) {
	rows, err := s.DB.Query(`SELECT id FROM households`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) UpdateHouseholdSettings(h *model.Household) error {
	_, err := s.DB.Exec(`UPDATE households SET name = ?, split_method = ?, include_variable_income = ?,
		display_currency = ?, manual_gold_price_cents = ? WHERE id = ?`,
		h.Name, h.SplitMethod, h.IncludeVariableIncome, h.DisplayCurrency, h.ManualGoldPriceCents, h.ID)
	return err
}

func (s *Store) RegenerateInvite(householdID int64) (string, error) {
	code := NewInviteCode()
	_, err := s.DB.Exec(`UPDATE households SET invite_code = ? WHERE id = ?`, code, householdID)
	return code, err
}

// --- transfer confirmations ---

func (s *Store) ConfirmTransfers(householdID int64, payloadJSON string) error {
	_, err := s.DB.Exec(`INSERT INTO transfer_confirmations (household_id, payload, confirmed_at) VALUES (?, ?, ?)`,
		householdID, payloadJSON, now())
	return err
}

// LastTransferConfirmation returns the latest confirmation, or ErrNotFound.
func (s *Store) LastTransferConfirmation(householdID int64) (*model.TransferConfirmation, error) {
	var c model.TransferConfirmation
	err := s.DB.QueryRow(`SELECT id, household_id, payload, confirmed_at FROM transfer_confirmations
		WHERE household_id = ? ORDER BY confirmed_at DESC, id DESC LIMIT 1`, householdID).
		Scan(&c.ID, &c.HouseholdID, &c.Payload, &c.ConfirmedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}
