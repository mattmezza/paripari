package store

import "github.com/mattmezza/paripari/internal/model"

const savedProjectionCols = `id, household_id, name, params, created_at`

func (s *Store) CreateSavedProjection(p *model.SavedProjection) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO saved_projections (household_id, name, params, created_at)
		VALUES (?, ?, ?, ?)`, p.HouseholdID, p.Name, p.Params, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateSavedProjection(p *model.SavedProjection) error {
	_, err := s.DB.Exec(`UPDATE saved_projections SET name = ?, params = ? WHERE id = ? AND household_id = ?`,
		p.Name, p.Params, p.ID, p.HouseholdID)
	return err
}

func (s *Store) DeleteSavedProjection(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM saved_projections WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) SavedProjections(householdID int64) ([]model.SavedProjection, error) {
	rows, err := s.DB.Query(`SELECT `+savedProjectionCols+`
		FROM saved_projections WHERE household_id = ? ORDER BY created_at, id`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SavedProjection
	for rows.Next() {
		var p model.SavedProjection
		if err := rows.Scan(&p.ID, &p.HouseholdID, &p.Name, &p.Params, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
