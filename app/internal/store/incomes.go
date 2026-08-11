package store

import "github.com/mattmezza/paripari/internal/model"

const incomeCols = `id, household_id, user_id, name, kind, pay_structure, gross_yearly_cents, currency, created_at`

func (s *Store) CreateIncome(in *model.IncomeSource) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO income_sources
		(household_id, user_id, name, kind, pay_structure, gross_yearly_cents, currency, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		in.HouseholdID, in.UserID, in.Name, in.Kind, in.PayStructure, in.GrossYearlyCents, in.Currency, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateIncome(in *model.IncomeSource) error {
	_, err := s.DB.Exec(`UPDATE income_sources SET user_id = ?, name = ?, kind = ?, pay_structure = ?,
		gross_yearly_cents = ?, currency = ? WHERE id = ? AND household_id = ?`,
		in.UserID, in.Name, in.Kind, in.PayStructure, in.GrossYearlyCents, in.Currency, in.ID, in.HouseholdID)
	return err
}

func (s *Store) DeleteIncome(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM income_sources WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) Income(householdID, id int64) (*model.IncomeSource, error) {
	var in model.IncomeSource
	err := s.DB.QueryRow(`SELECT `+incomeCols+` FROM income_sources WHERE id = ? AND household_id = ?`, id, householdID).
		Scan(&in.ID, &in.HouseholdID, &in.UserID, &in.Name, &in.Kind, &in.PayStructure,
			&in.GrossYearlyCents, &in.Currency, &in.CreatedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	in.Deductions, err = s.Deductions(in.ID)
	return &in, err
}

// Incomes returns all income sources of a household with their deductions loaded.
func (s *Store) Incomes(householdID int64) ([]model.IncomeSource, error) {
	rows, err := s.DB.Query(`SELECT `+incomeCols+` FROM income_sources WHERE household_id = ? ORDER BY user_id, id`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.IncomeSource
	for rows.Next() {
		var in model.IncomeSource
		if err := rows.Scan(&in.ID, &in.HouseholdID, &in.UserID, &in.Name, &in.Kind, &in.PayStructure,
			&in.GrossYearlyCents, &in.Currency, &in.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Deductions, err = s.Deductions(out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) Deductions(incomeID int64) ([]model.IncomeDeduction, error) {
	rows, err := s.DB.Query(`SELECT id, income_source_id, name, amount_cents, period, percent_bp
		FROM income_deductions WHERE income_source_id = ? ORDER BY id`, incomeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.IncomeDeduction
	for rows.Next() {
		var d model.IncomeDeduction
		if err := rows.Scan(&d.ID, &d.IncomeSourceID, &d.Name, &d.AmountCents, &d.Period, &d.PercentBP); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) CreateDeduction(d *model.IncomeDeduction) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO income_deductions (income_source_id, name, amount_cents, period, percent_bp)
		VALUES (?, ?, ?, ?, ?)`, d.IncomeSourceID, d.Name, d.AmountCents, d.Period, d.PercentBP)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateDeduction(d *model.IncomeDeduction) error {
	_, err := s.DB.Exec(`UPDATE income_deductions SET name = ?, amount_cents = ?, period = ?, percent_bp = ? WHERE id = ?`,
		d.Name, d.AmountCents, d.Period, d.PercentBP, d.ID)
	return err
}

func (s *Store) DeleteDeduction(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM income_deductions WHERE id = ?`, id)
	return err
}
