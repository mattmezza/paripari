package store

import "github.com/mattmezza/paripari/internal/model"

// PutSnapshot upserts today's (or the given date's) net worth snapshot. All amounts in CHF cents.
func (s *Store) PutSnapshot(sn *model.NetWorthSnapshot) error {
	date := sn.Date
	if date == "" {
		date = today()
	}
	_, err := s.DB.Exec(`INSERT INTO net_worth_snapshots
		(household_id, date, liquid_cents, alternative_cents, real_estate_cents) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(household_id, date) DO UPDATE SET
			liquid_cents = excluded.liquid_cents,
			alternative_cents = excluded.alternative_cents,
			real_estate_cents = excluded.real_estate_cents`,
		sn.HouseholdID, date, sn.LiquidCents, sn.AlternativeCents, sn.RealEstateCents)
	return err
}

// Snapshots returns snapshots oldest-first (limit <= 0 means all).
func (s *Store) Snapshots(householdID int64, limit int) ([]model.NetWorthSnapshot, error) {
	q := `SELECT id, household_id, date, liquid_cents, alternative_cents, real_estate_cents
		FROM net_worth_snapshots WHERE household_id = ? ORDER BY date`
	args := []any{householdID}
	if limit > 0 {
		q = `SELECT * FROM (` + q + ` DESC LIMIT ?) ORDER BY date`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.NetWorthSnapshot
	for rows.Next() {
		var sn model.NetWorthSnapshot
		if err := rows.Scan(&sn.ID, &sn.HouseholdID, &sn.Date, &sn.LiquidCents,
			&sn.AlternativeCents, &sn.RealEstateCents); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *Store) LatestSnapshot(householdID int64) (*model.NetWorthSnapshot, error) {
	var sn model.NetWorthSnapshot
	err := s.DB.QueryRow(`SELECT id, household_id, date, liquid_cents, alternative_cents, real_estate_cents
		FROM net_worth_snapshots WHERE household_id = ? ORDER BY date DESC LIMIT 1`, householdID).
		Scan(&sn.ID, &sn.HouseholdID, &sn.Date, &sn.LiquidCents, &sn.AlternativeCents, &sn.RealEstateCents)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &sn, nil
}
