package store

import "github.com/mattmezza/paripari/internal/model"

const goldCols = `id, household_id, name, weight_grams, purity_karat, quantity, location, created_at`

func (s *Store) CreateGoldItem(g *model.GoldItem) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO gold_items
		(household_id, name, weight_grams, purity_karat, quantity, location, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		g.HouseholdID, g.Name, g.WeightGrams, g.PurityKarat, g.Quantity, g.Location, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateGoldItem(g *model.GoldItem) error {
	_, err := s.DB.Exec(`UPDATE gold_items SET name = ?, weight_grams = ?, purity_karat = ?, quantity = ?,
		location = ? WHERE id = ? AND household_id = ?`,
		g.Name, g.WeightGrams, g.PurityKarat, g.Quantity, g.Location, g.ID, g.HouseholdID)
	return err
}

func (s *Store) DeleteGoldItem(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM gold_items WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) GoldItem(householdID, id int64) (*model.GoldItem, error) {
	var g model.GoldItem
	err := s.DB.QueryRow(`SELECT `+goldCols+` FROM gold_items WHERE id = ? AND household_id = ?`, id, householdID).
		Scan(&g.ID, &g.HouseholdID, &g.Name, &g.WeightGrams, &g.PurityKarat, &g.Quantity, &g.Location, &g.CreatedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &g, nil
}

func (s *Store) GoldItems(householdID int64) ([]model.GoldItem, error) {
	rows, err := s.DB.Query(`SELECT `+goldCols+` FROM gold_items WHERE household_id = ? ORDER BY id`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.GoldItem
	for rows.Next() {
		var g model.GoldItem
		if err := rows.Scan(&g.ID, &g.HouseholdID, &g.Name, &g.WeightGrams, &g.PurityKarat,
			&g.Quantity, &g.Location, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
