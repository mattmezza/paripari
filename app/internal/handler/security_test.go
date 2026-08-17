package handler_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/pquerna/otp/totp"
)

// recoveryCodeRe matches the "XXXX-XXXX-XXXX-XXXX-XXXX" display form.
var recoveryCodeRe = regexp.MustCompile(`[A-Z2-9]{4}(?:-[A-Z2-9]{4}){4}`)

func pendingCookie(t *testing.T, w interface{ Result() *http.Response }) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.PendingCookieName {
			return c
		}
	}
	t.Fatalf("no pending 2FA cookie in response")
	return nil
}

func TestTwoFactorEnrollConfirmLogin(t *testing.T) {
	h, st := newServerStore(t)
	c, _ := signup(t, h, st, "2fa@example.com")

	// Enable → the session is fresh, so a step-up form comes back first.
	if w := do(t, h, http.MethodPost, "/settings/2fa/enroll", nil, c); w.Code != http.StatusOK {
		t.Fatalf("enroll: %d: %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), "Confirm it's you") {
		t.Fatalf("enroll did not demand step-up: %s", w.Body.String())
	}

	// Step-up with next=enroll-2fa completes enrollment setup and shows the QR.
	if w := do(t, h, http.MethodPost, "/settings/step-up", url.Values{
		"password": {"paripari123"}, "next": {"enroll-2fa"},
	}, c); w.Code != http.StatusOK {
		t.Fatalf("step-up enroll: %d: %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), "data:image/png;base64,") {
		t.Fatalf("no QR code rendered: %s", w.Body.String())
	}

	u, err := st.UserByEmail("2fa@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.TOTPSecret == "" || u.TOTPEnabled {
		t.Fatalf("enrollment state = secret %q enabled %v", u.TOTPSecret, u.TOTPEnabled)
	}

	// A wrong code is refused…
	code, _ := totp.GenerateCode(u.TOTPSecret, time.Now().Add(-time.Minute))
	w := do(t, h, http.MethodPost, "/settings/2fa/confirm", url.Values{"code": {code}}, c)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("confirm with stale code: %d", w.Code)
	}

	// …the current code activates 2FA and shows the recovery codes once.
	code, _ = totp.GenerateCode(u.TOTPSecret, time.Now())
	w = do(t, h, http.MethodPost, "/settings/2fa/confirm", url.Values{"code": {code}}, c)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm: %d: %s", w.Code, w.Body.String())
	}
	codes := recoveryCodeRe.FindAllString(w.Body.String(), -1)
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("confirm showed %d recovery codes, want %d", len(codes), auth.RecoveryCodeCount)
	}
	u, _ = st.UserByEmail("2fa@example.com")
	if !u.TOTPEnabled || len(u.RecoveryCodes) != auth.RecoveryCodeCount {
		t.Fatalf("after confirm: enabled=%v codes=%d", u.TOTPEnabled, len(u.RecoveryCodes))
	}

	// Log out and sign back in: password alone must not open a session.
	do(t, h, http.MethodPost, "/logout", nil, c)
	if w := post(t, h, "/login", url.Values{
		"email": {"2fa@example.com"}, "password": {"paripari123"},
	}, nil); w.Code != http.StatusOK {
		t.Fatalf("login without code: %d: %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), "Two-factor authentication") {
		t.Fatalf("login did not ask for a code: %s", w.Body.String())
	}

	// The pending cookie plus a valid code completes the login.
	p := pendingCookie(t, post(t, h, "/login", url.Values{
		"email": {"2fa@example.com"}, "password": {"paripari123"},
	}, nil))
	code, _ = totp.GenerateCode(u.TOTPSecret, time.Now())
	if w := post(t, h, "/login", url.Values{
		"email": {"2fa@example.com"}, "code": {code},
	}, p); w.Code != http.StatusSeeOther {
		t.Fatalf("login with code: %d: %s", w.Code, w.Body.String())
	} else {
		sessionCookie(t, w)
	}
}

func TestLoginWithRecoveryCodeConsumesIt(t *testing.T) {
	h, st := newServerStore(t)
	c, _ := signup(t, h, st, "rc@example.com")

	// Fast path: step up once, then enroll directly (already verified).
	do(t, h, http.MethodPost, "/settings/step-up", url.Values{
		"password": {"paripari123"}, "next": {"enroll-2fa"},
	}, c)
	u, _ := st.UserByEmail("rc@example.com")
	code, _ := totp.GenerateCode(u.TOTPSecret, time.Now())
	w := do(t, h, http.MethodPost, "/settings/2fa/confirm", url.Values{"code": {code}}, c)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm: %d: %s", w.Code, w.Body.String())
	}
	// The confirm response shows the plaintext codes exactly once; the DB only
	// ever holds their hashes, so the login must use the displayed form.
	codes := recoveryCodeRe.FindAllString(w.Body.String(), -1)
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("confirm showed %d codes, want %d", len(codes), auth.RecoveryCodeCount)
	}
	backup := codes[0]

	u, _ = st.UserByEmail("rc@example.com")
	if len(u.RecoveryCodes) != auth.RecoveryCodeCount {
		t.Fatalf("stored digests = %d", len(u.RecoveryCodes))
	}

	do(t, h, http.MethodPost, "/logout", nil, c)
	p := pendingCookie(t, post(t, h, "/login", url.Values{
		"email": {"rc@example.com"}, "password": {"paripari123"},
	}, nil))

	// The recovery code — not a TOTP code — signs the user in.
	if w := post(t, h, "/login", url.Values{
		"email": {"rc@example.com"}, "code": {backup},
	}, p); w.Code != http.StatusSeeOther {
		t.Fatalf("login with recovery code: %d: %s", w.Code, w.Body.String())
	}

	// And it is single-use: the stored list lost exactly that entry.
	u, _ = st.UserByEmail("rc@example.com")
	if len(u.RecoveryCodes) != auth.RecoveryCodeCount-1 {
		t.Fatalf("recovery codes left = %d, want %d", len(u.RecoveryCodes), auth.RecoveryCodeCount-1)
	}
	for _, have := range u.RecoveryCodes {
		if have == backup {
			t.Fatal("used recovery code is still in the list")
		}
	}
}

func TestExportRequiresStepUp(t *testing.T) {
	h, st := newServerStore(t)
	c, _ := signup(t, h, st, "exp@example.com")

	// Without step-up the export endpoint serves the challenge page, not JSON.
	w := do(t, h, http.MethodGet, "/settings/export", nil, c)
	if cd := w.Header().Get("Content-Disposition"); strings.HasPrefix(cd, "attachment") {
		t.Fatalf("export without step-up served the file (cd=%q)", cd)
	}
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Confirm it's you") {
		t.Fatalf("export without step-up did not show the challenge: %d: %s", w.Code, w.Body.String())
	}

	// Wrong password keeps the gate closed.
	if w := do(t, h, http.MethodPost, "/settings/step-up", url.Values{
		"password": {"nope"}, "next": {"export"},
	}, c); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("step-up with bad password: %d", w.Code)
	}

	// Correct credentials open it and the redirect serves the file.
	if w := do(t, h, http.MethodPost, "/settings/step-up", url.Values{
		"password": {"paripari123"}, "next": {"export"},
	}, c); w.Code != http.StatusSeeOther {
		t.Fatalf("step-up: %d: %s", w.Code, w.Body.String())
	}
	w = do(t, h, http.MethodGet, "/settings/export", nil, c)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export after step-up: %d cd=%q", w.Code, w.Header().Get("Content-Disposition"))
	}
}

func TestPasswordChangeRequiresStepUpAndRevokesOthers(t *testing.T) {
	h, st := newServerStore(t)
	c, _ := signup(t, h, st, "pw@example.com")

	// A second device: another session for the same user.
	w2 := post(t, h, "/login", url.Values{
		"email": {"pw@example.com"}, "password": {"paripari123"},
	}, nil)
	c2 := sessionCookie(t, w2)

	// Password change without step-up bounces to the challenge.
	if w := do(t, h, http.MethodPost, "/settings/password", url.Values{
		"current_password": {"paripari123"}, "new_password": {"brandnew123"},
	}, c); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Confirm it's you") {
		t.Fatalf("password change without step-up: %d: %s", w.Code, w.Body.String())
	}

	// Complete the change through the step-up form (it carries the new password).
	if w := do(t, h, http.MethodPost, "/settings/step-up", url.Values{
		"password": {"paripari123"}, "next": {"password"}, "new_password": {"brandnew123"},
	}, c); w.Code != http.StatusSeeOther {
		t.Fatalf("step-up password change: %d: %s", w.Code, w.Body.String())
	}

	// Current session survives, the other device is dead.
	if w := do(t, h, http.MethodGet, "/", nil, c); w.Code != http.StatusOK {
		t.Fatalf("current session after password change: %d", w.Code)
	}
	if w := do(t, h, http.MethodGet, "/", nil, c2); w.Code != http.StatusSeeOther {
		t.Fatalf("other session after password change: %d, want 303", w.Code)
	}

	// Old password gone, new one works.
	if w := post(t, h, "/login", url.Values{
		"email": {"pw@example.com"}, "password": {"paripari123"},
	}, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("login with old password: %d", w.Code)
	}
	if w := post(t, h, "/login", url.Values{
		"email": {"pw@example.com"}, "password": {"brandnew123"},
	}, nil); w.Code != http.StatusSeeOther {
		t.Fatalf("login with new password: %d", w.Code)
	}
}

func TestSessionExpirySettingAppliesImmediately(t *testing.T) {
	h, st := newServerStore(t)
	c, _ := signup(t, h, st, "expiry@example.com")

	// Default is 30 days on the cookie.
	if c.MaxAge != int(30*24*3600) {
		t.Fatalf("default cookie Max-Age = %d, want %d", c.MaxAge, int(30*24*3600))
	}

	// Switch to 7 days: the response must re-stamp the current session.
	w := do(t, h, http.MethodPost, "/settings/session-expiry", url.Values{
		"session_expiry_days": {"7"},
	}, c)
	if w.Code != http.StatusOK {
		t.Fatalf("set 7 days: %d: %s", w.Code, w.Body.String())
	}
	got := sessionCookie(t, w)
	if got.MaxAge != int(7*24*3600) {
		t.Fatalf("cookie after 7-day setting = %d", got.MaxAge)
	}
	var exp *string
	if err := st.DB.QueryRow(`SELECT expires_at FROM sessions WHERE token = ?`, c.Value).Scan(&exp); err != nil {
		t.Fatal(err)
	}
	if exp == nil {
		t.Fatal("expires_at is NULL after 7-day setting")
	}
	if e, err := time.Parse("2006-01-02T15:04:05Z", *exp); err != nil {
		t.Fatal(err)
	} else if d := time.Until(e); d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Fatalf("row expires in %v, want ~7 days", d)
	}

	// Never: the row becomes NULL and the cookie goes long-lived.
	w = do(t, h, http.MethodPost, "/settings/session-expiry", url.Values{
		"session_expiry_days": {"never"},
	}, c)
	if got := sessionCookie(t, w); got.MaxAge < 5*365*24*3600 {
		t.Fatalf("never cookie Max-Age = %d", got.MaxAge)
	}
	if err := st.DB.QueryRow(`SELECT expires_at FROM sessions WHERE token = ?`, c.Value).Scan(&exp); err != nil {
		t.Fatal(err)
	}
	if exp != nil {
		t.Fatalf("expires_at = %q after never, want NULL", *exp)
	}

	// The preference itself is persisted per user.
	u, _ := st.UserByEmail("expiry@example.com")
	if u.SessionExpiryDays != nil {
		t.Fatalf("stored preference = %d, want NULL", *u.SessionExpiryDays)
	}
}
