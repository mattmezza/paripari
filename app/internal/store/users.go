package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/mattmezza/paripari/internal/model"
)

const userCols = `id, household_id, email, password_hash, name, created_at, session_expiry_days, totp_secret, totp_enabled, recovery_codes`

// userColsPrefixed is userCols for queries that join another table carrying
// the same column names (sessions, pending_logins).
const userColsPrefixed = `u.id, u.household_id, u.email, u.password_hash, u.name, u.created_at, u.session_expiry_days, u.totp_secret, u.totp_enabled, u.recovery_codes`

func scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	var exp sql.NullInt64
	var totpSecret sql.NullString
	var codes sql.NullString
	err := row.Scan(&u.ID, &u.HouseholdID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt,
		&exp, &totpSecret, &u.TOTPEnabled, &codes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if exp.Valid {
		n := int(exp.Int64)
		u.SessionExpiryDays = &n
	}
	u.TOTPSecret = totpSecret.String
	if codes.Valid && codes.String != "" {
		if err := json.Unmarshal([]byte(codes.String), &u.RecoveryCodes); err != nil {
			return nil, err
		}
	}
	return &u, nil
}

func scanUserRows(rows *sql.Rows) ([]model.User, error) {
	var out []model.User
	for rows.Next() {
		var u model.User
		var exp sql.NullInt64
		var totpSecret sql.NullString
		var codes sql.NullString
		if err := rows.Scan(&u.ID, &u.HouseholdID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt,
			&exp, &totpSecret, &u.TOTPEnabled, &codes); err != nil {
			return nil, err
		}
		if exp.Valid {
			n := int(exp.Int64)
			u.SessionExpiryDays = &n
		}
		u.TOTPSecret = totpSecret.String
		if codes.Valid && codes.String != "" {
			if err := json.Unmarshal([]byte(codes.String), &u.RecoveryCodes); err != nil {
				return nil, err
			}
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func recoveryCodesJSON(codes []string) (any, error) {
	if codes == nil {
		return nil, nil
	}
	return json.Marshal(codes)
}

// defaultSessionExpiryDays is what new users get; the "never" choice is NULL.
const defaultSessionExpiryDays = 30

func (s *Store) CreateUser(householdID int64, email, passwordHash, name string) (*model.User, error) {
	res, err := s.DB.Exec(`INSERT INTO users (household_id, email, password_hash, name, created_at, session_expiry_days)
		VALUES (?, ?, ?, ?, ?, ?)`, householdID, email, passwordHash, name, now(), defaultSessionExpiryDays)
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
	return scanUserRows(rows)
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

func (s *Store) UpdateUserName(householdID, id int64, name string) error {
	_, err := s.DB.Exec(`UPDATE users SET name = ? WHERE id = ? AND household_id = ?`, name, id, householdID)
	return err
}

// UpdateUserSessionExpiry sets the user's session lifetime. days == nil means
// "never expire". It only changes future sessions; the current one is updated
// by the handler via UpdateSessionExpiry.
func (s *Store) UpdateUserSessionExpiry(id int64, days *int) error {
	_, err := s.DB.Exec(`UPDATE users SET session_expiry_days = ? WHERE id = ?`, days, id)
	return err
}

// --- sessions ---

// CreateSession inserts a session row. expiresAt is the ISO8601 expiry, or
// nil for a never-expiring session.
func (s *Store) CreateSession(token string, userID int64, expiresAt *string) error {
	_, err := s.DB.Exec(`INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, now(), expiresAt)
	return err
}

// SessionUser returns the user and session row for a non-expired session
// token. A NULL expires_at is treated as never-expired.
func (s *Store) SessionUser(token string) (*model.User, *model.Session, error) {
	var u model.User
	var sess model.Session
	var exp sql.NullInt64
	var totpSecret sql.NullString
	var codes sql.NullString
	var expiresAt, verifiedAt sql.NullString
	err := s.DB.QueryRow(`SELECT `+userColsPrefixed+`, s.expires_at, s.verified_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND (s.expires_at IS NULL OR s.expires_at > ?)`, token, now()).
		Scan(&u.ID, &u.HouseholdID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt,
			&exp, &totpSecret, &u.TOTPEnabled, &codes, &expiresAt, &verifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if exp.Valid {
		n := int(exp.Int64)
		u.SessionExpiryDays = &n
	}
	u.TOTPSecret = totpSecret.String
	if codes.Valid && codes.String != "" {
		if err := json.Unmarshal([]byte(codes.String), &u.RecoveryCodes); err != nil {
			return nil, nil, err
		}
	}
	sess.Token = token
	sess.ExpiresAt = expiresAt.String
	sess.VerifiedAt = verifiedAt.String
	return &u, &sess, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// DeleteOtherSessions revokes every session of the user except the given one
// (used after a password change).
func (s *Store) DeleteOtherSessions(token string, userID int64) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE user_id = ? AND token != ?`, userID, token)
	return err
}

// UpdateSessionExpiry re-stamps a session's expiry (sliding renewal, and the
// "apply the new lifetime to the current session" path). expiresAt nil = never.
func (s *Store) UpdateSessionExpiry(token string, expiresAt *string) error {
	_, err := s.DB.Exec(`UPDATE sessions SET expires_at = ? WHERE token = ?`, expiresAt, token)
	return err
}

// UpdateSessionVerifiedAt stamps the step-up re-auth marker.
func (s *Store) UpdateSessionVerifiedAt(token, verifiedAt string) error {
	_, err := s.DB.Exec(`UPDATE sessions SET verified_at = ? WHERE token = ?`, verifiedAt, token)
	return err
}

func (s *Store) DeleteExpiredSessions() error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE expires_at IS NOT NULL AND expires_at <= ?`, now())
	return err
}

// --- TOTP ---

// SetTOTPEnrollment stores the pending enrollment secret (enabled stays false
// until a valid code is confirmed).
func (s *Store) SetTOTPSecret(userID int64, secret string) error {
	_, err := s.DB.Exec(`UPDATE users SET totp_secret = ? WHERE id = ?`, secret, userID)
	return err
}

// EnableTOTP activates the stored secret and replaces the recovery codes.
func (s *Store) EnableTOTP(userID int64, recoveryCodes []string) error {
	codes, err := recoveryCodesJSON(recoveryCodes)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE users SET totp_enabled = 1, recovery_codes = ? WHERE id = ?`, codes, userID)
	return err
}

// ReplaceRecoveryCodes rotates the single-use codes (regenerate).
func (s *Store) ReplaceRecoveryCodes(userID int64, recoveryCodes []string) error {
	codes, err := recoveryCodesJSON(recoveryCodes)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE users SET recovery_codes = ? WHERE id = ?`, codes, userID)
	return err
}

// ConsumeRecoveryCode removes one used code from the user's list.
func (s *Store) ConsumeRecoveryCode(userID int64, remaining []string) error {
	return s.ReplaceRecoveryCodes(userID, remaining)
}

// DisableTOTP clears the secret, the enabled flag and the recovery codes.
func (s *Store) DisableTOTP(userID int64) error {
	_, err := s.DB.Exec(`UPDATE users SET totp_secret = NULL, totp_enabled = 0, recovery_codes = NULL WHERE id = ?`, userID)
	return err
}

// --- pending logins (the 2FA half-login) ---

func (s *Store) CreatePendingLogin(token string, userID int64, expiresAt string) error {
	_, err := s.DB.Exec(`INSERT INTO pending_logins (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, now(), expiresAt)
	return err
}

// PendingLoginUser resolves a pending-login token to its user, or ErrNotFound
// when it is unknown or expired.
func (s *Store) PendingLoginUser(token string) (*model.User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userColsPrefixed+`
		FROM pending_logins p JOIN users u ON u.id = p.user_id
		WHERE p.token = ? AND p.expires_at > ?`, token, now()))
}

func (s *Store) DeletePendingLogin(token string) error {
	_, err := s.DB.Exec(`DELETE FROM pending_logins WHERE token = ?`, token)
	return err
}

// DeleteExpiredPendingLogins is the daily GC companion to DeleteExpiredSessions.
func (s *Store) DeleteExpiredPendingLogins() error {
	_, err := s.DB.Exec(`DELETE FROM pending_logins WHERE expires_at <= ?`, now())
	return err
}
