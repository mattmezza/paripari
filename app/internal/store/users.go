package store

import (
	"database/sql"
	"errors"

	"github.com/mattmezza/paripari/internal/model"
)

const userCols = `id, household_id, email, password_hash, name, created_at`

func scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.HouseholdID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) CreateUser(householdID int64, email, passwordHash, name string) (*model.User, error) {
	res, err := s.DB.Exec(`INSERT INTO users (household_id, email, password_hash, name, created_at)
		VALUES (?, ?, ?, ?, ?)`, householdID, email, passwordHash, name, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.User(id)
}

func (s *Store) User(id int64) (*model.User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) UserByEmail(email string) (*model.User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

func (s *Store) UsersByHousehold(householdID int64) ([]model.User, error) {
	rows, err := s.DB.Query(`SELECT `+userCols+` FROM users WHERE household_id = ? ORDER BY id`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.HouseholdID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CountUsers(householdID int64) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE household_id = ?`, householdID).Scan(&n)
	return n, err
}

func (s *Store) UpdateUserPassword(id int64, hash string) error {
	_, err := s.DB.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

func (s *Store) UpdateUserName(id int64, name string) error {
	_, err := s.DB.Exec(`UPDATE users SET name = ? WHERE id = ?`, name, id)
	return err
}

// --- sessions ---

func (s *Store) CreateSession(token string, userID int64, expiresAt string) error {
	_, err := s.DB.Exec(`INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, now(), expiresAt)
	return err
}

// SessionUser returns the user for a non-expired session token.
func (s *Store) SessionUser(token string) (*model.User, error) {
	return scanUser(s.DB.QueryRow(`SELECT u.`+`id, u.household_id, u.email, u.password_hash, u.name, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > ?`, token, now()))
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) DeleteExpiredSessions() error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, now())
	return err
}
