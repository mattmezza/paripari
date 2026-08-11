package store

import "github.com/mattmezza/paripari/internal/model"

const goalCols = `id, household_id, name, target_amount_cents, currency, deadline, category, created_at`

func (s *Store) CreateGoal(g *model.Goal) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO goals (household_id, name, target_amount_cents, currency, deadline, category, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		g.HouseholdID, g.Name, g.TargetAmountCents, g.Currency, g.Deadline, g.Category, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateGoal(g *model.Goal) error {
	_, err := s.DB.Exec(`UPDATE goals SET name = ?, target_amount_cents = ?, currency = ?, deadline = ?, category = ?
		WHERE id = ? AND household_id = ?`,
		g.Name, g.TargetAmountCents, g.Currency, g.Deadline, g.Category, g.ID, g.HouseholdID)
	return err
}

func (s *Store) DeleteGoal(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM goals WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) Goal(householdID, id int64) (*model.Goal, error) {
	var g model.Goal
	err := s.DB.QueryRow(`SELECT `+goalCols+` FROM goals WHERE id = ? AND household_id = ?`, id, householdID).
		Scan(&g.ID, &g.HouseholdID, &g.Name, &g.TargetAmountCents, &g.Currency, &g.Deadline, &g.Category, &g.CreatedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &g, nil
}

func (s *Store) Goals(householdID int64) ([]model.Goal, error) {
	rows, err := s.DB.Query(`SELECT `+goalCols+` FROM goals WHERE household_id = ? ORDER BY deadline IS NULL, deadline, name`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Goal
	for rows.Next() {
		var g model.Goal
		if err := rows.Scan(&g.ID, &g.HouseholdID, &g.Name, &g.TargetAmountCents, &g.Currency,
			&g.Deadline, &g.Category, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
