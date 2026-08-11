package store

import "github.com/mattmezza/paripari/internal/model"

const scenarioChangeCols = `id, scenario_id, change_type, target_id, label, value_cents, value_num, value_text, currency`

func (s *Store) CreateScenario(sc *model.Scenario) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO scenarios (household_id, name, description, created_at) VALUES (?, ?, ?, ?)`,
		sc.HouseholdID, sc.Name, sc.Description, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateScenario(sc *model.Scenario) error {
	_, err := s.DB.Exec(`UPDATE scenarios SET name = ?, description = ? WHERE id = ? AND household_id = ?`,
		sc.Name, sc.Description, sc.ID, sc.HouseholdID)
	return err
}

func (s *Store) DeleteScenario(householdID, id int64) error {
	_, err := s.DB.Exec(`DELETE FROM scenarios WHERE id = ? AND household_id = ?`, id, householdID)
	return err
}

func (s *Store) Scenario(householdID, id int64) (*model.Scenario, error) {
	var sc model.Scenario
	err := s.DB.QueryRow(`SELECT id, household_id, name, description, created_at FROM scenarios
		WHERE id = ? AND household_id = ?`, id, householdID).
		Scan(&sc.ID, &sc.HouseholdID, &sc.Name, &sc.Description, &sc.CreatedAt)
	if err != nil {
		return nil, mapNoRows(err)
	}
	sc.Changes, err = s.ScenarioChanges(sc.ID)
	return &sc, err
}

// Scenarios returns all scenarios of a household with their changes loaded.
func (s *Store) Scenarios(householdID int64) ([]model.Scenario, error) {
	rows, err := s.DB.Query(`SELECT id, household_id, name, description, created_at FROM scenarios
		WHERE household_id = ? ORDER BY created_at DESC, id DESC`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Scenario
	for rows.Next() {
		var sc model.Scenario
		if err := rows.Scan(&sc.ID, &sc.HouseholdID, &sc.Name, &sc.Description, &sc.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Changes, err = s.ScenarioChanges(out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) ScenarioChanges(scenarioID int64) ([]model.ScenarioChange, error) {
	rows, err := s.DB.Query(`SELECT `+scenarioChangeCols+` FROM scenario_changes WHERE scenario_id = ? ORDER BY id`, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ScenarioChange
	for rows.Next() {
		var c model.ScenarioChange
		if err := rows.Scan(&c.ID, &c.ScenarioID, &c.ChangeType, &c.TargetID, &c.Label,
			&c.ValueCents, &c.ValueNum, &c.ValueText, &c.Currency); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateScenarioChange(c *model.ScenarioChange) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO scenario_changes
		(scenario_id, change_type, target_id, label, value_cents, value_num, value_text, currency)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ScenarioID, c.ChangeType, c.TargetID, c.Label, c.ValueCents, c.ValueNum, c.ValueText, c.Currency)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteScenarioChange(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM scenario_changes WHERE id = ?`, id)
	return err
}
