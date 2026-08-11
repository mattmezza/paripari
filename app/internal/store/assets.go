package store

import "github.com/mattmezza/paripari/internal/model"

const assetCols = `id, household_id, name, kind, estimated_value_cents, currency, created_at`

func (s *Store) CreateAsset(a *model.Asset) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO assets (household_id, name, kind, estimated_value_cents, currency, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, a.HouseholdID, a.Name, a.Kind, a.EstimatedValueCents, a.Currency, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateAsset(a *model.Asset) error {
	_, err := s.DB.Exec(`UPDATE assets SET name = ?, kind = ?, estimated_value_cents = ?, currency = ?
		WHERE id = ? AND household_id = ?`, a.Name, a.Kind, a.EstimatedValueCents, a.Currency, a.ID, a.HouseholdID)
	return err
}

func (s *Store) DeleteAsset(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM assets WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) Asset(householdID, id int64) (*model.Asset, error) {
	var a model.Asset
	err := s.DB.QueryRow(`SELECT `+assetCols+` FROM assets WHERE id = ? AND household_id = ?`, id, householdID).
		Scan(&a.ID, &a.HouseholdID, &a.Name, &a.Kind, &a.EstimatedValueCents, &a.Currency, &a.CreatedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &a, nil
}

func (s *Store) Assets(householdID int64) ([]model.Asset, error) {
	rows, err := s.DB.Query(`SELECT `+assetCols+` FROM assets WHERE household_id = ? ORDER BY kind, name`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Asset
	for rows.Next() {
		var a model.Asset
		if err := rows.Scan(&a.ID, &a.HouseholdID, &a.Name, &a.Kind, &a.EstimatedValueCents,
			&a.Currency, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
