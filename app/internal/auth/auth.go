// Package auth handles passwords, DB-backed sessions, signup/invite flow and
// the request middleware (auth + CSRF).
package auth

import (
	"errors"
	"strings"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrBadCredentials = errors.New("invalid email or password")
	ErrEmailTaken     = errors.New("that email is already registered")
	ErrBadInvite      = errors.New("unknown invite code")
	ErrHouseholdFull  = errors.New("this household already has two partners")
	ErrWeakPassword   = errors.New("password must be at least 8 characters")
	ErrMissingField   = errors.New("name, email and password are required")
)

const maxPartners = 2

func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

func VerifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// Signup creates a user. With an empty invite code it also creates a new
// household (and its invite code); with a code it joins that household, which
// is closed once it holds two partners.
func Signup(s *store.Store, name, email, password, invite string) (*model.User, error) {
	name, email = strings.TrimSpace(name), normalizeEmail(email)
	if name == "" || email == "" || password == "" {
		return nil, ErrMissingField
	}
	if len(password) < 8 {
		return nil, ErrWeakPassword
	}
	if _, err := s.UserByEmail(email); err == nil {
		return nil, ErrEmailTaken
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	var hh *model.Household
	invite = strings.ToUpper(strings.TrimSpace(invite))
	if invite == "" {
		var err error
		if hh, err = s.CreateHousehold(name + "'s household"); err != nil {
			return nil, err
		}
	} else {
		var err error
		hh, err = s.HouseholdByInvite(invite)
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrBadInvite
		} else if err != nil {
			return nil, err
		}
		n, err := s.CountUsers(hh.ID)
		if err != nil {
			return nil, err
		}
		if n >= maxPartners {
			return nil, ErrHouseholdFull
		}
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	return s.CreateUser(hh.ID, email, hash, name)
}

// Login verifies credentials and returns the user.
func Login(s *store.Store, email, password string) (*model.User, error) {
	u, err := s.UserByEmail(normalizeEmail(email))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrBadCredentials
	} else if err != nil {
		return nil, err
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return nil, ErrBadCredentials
	}
	return u, nil
}
