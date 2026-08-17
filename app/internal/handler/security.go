package handler

import (
	"net/http"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/view"
)

// twoFactorState is what the settings-security partial renders.
type twoFactorState struct {
	Enrolled bool
	Pending  bool   // secret generated, awaiting the activation code
	Secret   string // base32 secret, shown next to the QR while pending
	QRData   string // data:image/png;base64,… QR code
	// RecoveryCodes carries plaintext codes for one-time display right after
	// activation or regeneration; empty on every other render.
	RecoveryCodes []string
	Remaining     int // unused recovery codes left
}

// stepUpData drives the "confirm it's you" form.
type stepUpData struct {
	Next string // export | password | disable-2fa | codes | enroll-2fa
}

// renderSettingsWith renders the full settings page and lets the caller tweak
// the data after the standard build.
func renderSettingsWith(d *Deps, w http.ResponseWriter, r *http.Request, notice, errMsg string, status int, mutate func(*settingsData)) {
	data, err := buildSettingsData(d, r)
	if err != nil {
		http.Error(w, "could not load settings", http.StatusInternalServerError)
		return
	}
	if mutate != nil {
		mutate(data)
	}
	data.Notice, data.Err = notice, errMsg
	d.View.RenderStatus(w, r, status, "settings", &view.PageData{Title: "Settings", Data: data})
}

// renderStepUp replaces the account/security cards with the step-up form.
func renderStepUp(d *Deps, w http.ResponseWriter, r *http.Request, next, errMsg string, status int) {
	renderSettingsWith(d, w, r, "", errMsg, status, func(data *settingsData) {
		data.StepUp = &stepUpData{Next: next}
	})
}

// securityPartial renders the security card, optionally with fresh plaintext
// recovery codes for their one-time display.
func securityPartial(d *Deps, w http.ResponseWriter, r *http.Request, notice, errMsg string, status int, showCodes []string) {
	data, err := buildSettingsData(d, r)
	if err != nil {
		http.Error(w, "could not load settings", http.StatusInternalServerError)
		return
	}
	data.Notice, data.Err = notice, errMsg
	if showCodes != nil {
		data.TwoFactor.RecoveryCodes = showCodes
	}
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	d.View.Partial(w, "partials/settings-security", data)
}

// stepUpVerifies checks the step-up credentials: the current password, plus a
// TOTP or recovery code when 2FA is enabled. Returns an error message or "".
func stepUpVerifies(d *Deps, sess *auth.Session, r *http.Request) string {
	if sess.User == nil || !auth.VerifyPassword(sess.User.PasswordHash, r.PostFormValue("password")) {
		return "That password isn't right."
	}
	if !sess.User.TOTPEnabled {
		return ""
	}
	code := r.PostFormValue("code")
	if auth.ValidateTOTP(sess.User.TOTPSecret, code) {
		return ""
	}
	if remaining, matched := auth.VerifyRecoveryCode(sess.User, code); matched {
		if err := d.Store.ConsumeRecoveryCode(sess.User.ID, remaining); err != nil {
			return "Could not update your recovery codes — try again."
		}
		sess.User.RecoveryCodes = remaining
		return ""
	}
	return "That code isn't right. Check your authenticator app, or use a recovery code."
}

// registerSecurity wires the two-factor and step-up endpoints. It needs the
// settings `partial` renderer, so it is called from registerSettings.
func registerSecurity(mux *http.ServeMux, d *Deps,
	partial func(w http.ResponseWriter, r *http.Request, name, notice, errMsg string, status int)) {

	// Begin enrollment: step-up first, then generate the secret and show the QR.
	mux.HandleFunc("POST /settings/2fa/enroll", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		if sess.User.TOTPEnabled {
			partial(w, r, "settings-security", "", "Two-factor authentication is already on.", http.StatusConflict)
			return
		}
		if auth.RequiresStepUp(sess) {
			renderStepUp(d, w, r, "enroll-2fa", "", http.StatusOK)
			return
		}
		secret, _, err := auth.GenerateTOTPSecret(sess.User.Email)
		if err != nil {
			http.Error(w, "could not set up two-factor authentication", http.StatusInternalServerError)
			return
		}
		if err := d.Store.SetTOTPSecret(sess.User.ID, secret); err != nil {
			http.Error(w, "could not set up two-factor authentication", http.StatusInternalServerError)
			return
		}
		sess.User.TOTPSecret = secret
		securityPartial(d, w, r, "Scan the QR code with your authenticator app, then enter the 6-digit code to turn it on.", "", http.StatusOK, nil)
	})

	// Activate: a valid code flips totp_enabled and mints the recovery codes.
	mux.HandleFunc("POST /settings/2fa/confirm", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		if sess.User.TOTPEnabled || sess.User.TOTPSecret == "" {
			partial(w, r, "settings-security", "", "Start enrollment first.", http.StatusConflict)
			return
		}
		if !auth.ValidateTOTP(sess.User.TOTPSecret, r.PostFormValue("code")) {
			partial(w, r, "settings-security", "", "That code isn't right — try the current one from your authenticator.", http.StatusUnprocessableEntity)
			return
		}
		codes, err := auth.GenerateRecoveryCodes()
		if err != nil {
			http.Error(w, "could not create recovery codes", http.StatusInternalServerError)
			return
		}
		hashes := make([]string, len(codes))
		for i := range codes {
			hashes[i] = auth.HashRecoveryCode(codes[i])
		}
		if err := d.Store.EnableTOTP(sess.User.ID, hashes); err != nil {
			http.Error(w, "could not enable two-factor authentication", http.StatusInternalServerError)
			return
		}
		sess.User.TOTPEnabled = true
		sess.User.RecoveryCodes = hashes
		securityPartial(d, w, r,
			"Two-factor authentication is on. Write down the recovery codes below — each works exactly once, and this is the only time they are shown.",
			"", http.StatusOK, codes)
	})

	// Disable: step-up (password + TOTP/recovery) first.
	mux.HandleFunc("POST /settings/2fa/disable", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		if auth.RequiresStepUp(sess) {
			renderStepUp(d, w, r, "disable-2fa", "", http.StatusOK)
			return
		}
		if err := d.Store.DisableTOTP(sess.User.ID); err != nil {
			http.Error(w, "could not disable two-factor authentication", http.StatusInternalServerError)
			return
		}
		sess.User.TOTPEnabled = false
		sess.User.TOTPSecret = ""
		sess.User.RecoveryCodes = nil
		securityPartial(d, w, r, "Two-factor authentication is off.", "", http.StatusOK, nil)
	})

	// Regenerate recovery codes: step-up first, new codes shown once.
	mux.HandleFunc("POST /settings/2fa/codes", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		if !sess.User.TOTPEnabled {
			partial(w, r, "settings-security", "", "Two-factor authentication is off.", http.StatusConflict)
			return
		}
		if auth.RequiresStepUp(sess) {
			renderStepUp(d, w, r, "codes", "", http.StatusOK)
			return
		}
		codes, err := auth.GenerateRecoveryCodes()
		if err != nil {
			http.Error(w, "could not create recovery codes", http.StatusInternalServerError)
			return
		}
		hashes := make([]string, len(codes))
		for i := range codes {
			hashes[i] = auth.HashRecoveryCode(codes[i])
		}
		if err := d.Store.ReplaceRecoveryCodes(sess.User.ID, hashes); err != nil {
			http.Error(w, "could not replace recovery codes", http.StatusInternalServerError)
			return
		}
		sess.User.RecoveryCodes = hashes
		securityPartial(d, w, r,
			"New recovery codes ready. The old ones no longer work — save these somewhere safe; they are shown only once.",
			"", http.StatusOK, codes)
	})

	// Step-up challenge: verify credentials, then complete the pending action.
	mux.HandleFunc("POST /settings/step-up", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		next := r.PostFormValue("next")
		if msg := stepUpVerifies(d, sess, r); msg != "" {
			renderStepUp(d, w, r, next, msg, http.StatusUnprocessableEntity)
			return
		}
		if err := d.Auth.MarkStepUp(w, r); err != nil {
			http.Error(w, "could not verify", http.StatusInternalServerError)
			return
		}

		switch next {
		case "export":
			// Freshly verified: the export endpoint now serves the file.
			http.Redirect(w, r, "/settings/export", http.StatusSeeOther)
		case "password":
			newPw := r.PostFormValue("new_password")
			if len(newPw) < 8 {
				renderStepUp(d, w, r, next, "New password must be at least 8 characters.", http.StatusUnprocessableEntity)
				return
			}
			hash, err := auth.HashPassword(newPw)
			if err != nil {
				http.Error(w, "could not set password", http.StatusInternalServerError)
				return
			}
			if err := d.Store.UpdateUserPassword(sess.User.ID, hash); err != nil {
				http.Error(w, "could not set password", http.StatusInternalServerError)
				return
			}
			// Every other device loses its session; the current one stays.
			if err := d.Store.DeleteOtherSessions(sess.Token, sess.User.ID); err != nil {
				http.Error(w, "could not revoke other sessions", http.StatusInternalServerError)
				return
			}
			view.SetFlash(w, "Password updated. Other devices were signed out.")
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
		case "disable-2fa":
			if err := d.Store.DisableTOTP(sess.User.ID); err != nil {
				http.Error(w, "could not disable two-factor authentication", http.StatusInternalServerError)
				return
			}
			sess.User.TOTPEnabled = false
			sess.User.TOTPSecret = ""
			sess.User.RecoveryCodes = nil
			view.SetFlash(w, "Two-factor authentication is off.")
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
		case "codes":
			codes, err := auth.GenerateRecoveryCodes()
			if err != nil {
				http.Error(w, "could not create recovery codes", http.StatusInternalServerError)
				return
			}
			hashes := make([]string, len(codes))
			for i := range codes {
				hashes[i] = auth.HashRecoveryCode(codes[i])
			}
			if err := d.Store.ReplaceRecoveryCodes(sess.User.ID, hashes); err != nil {
				http.Error(w, "could not replace recovery codes", http.StatusInternalServerError)
				return
			}
			sess.User.RecoveryCodes = hashes
			renderSettingsWith(d, w, r,
				"New recovery codes ready — shown once, save them somewhere safe.", "", http.StatusOK,
				func(data *settingsData) { data.TwoFactor.RecoveryCodes = codes })
		case "enroll-2fa":
			secret, _, err := auth.GenerateTOTPSecret(sess.User.Email)
			if err != nil {
				http.Error(w, "could not set up two-factor authentication", http.StatusInternalServerError)
				return
			}
			if err := d.Store.SetTOTPSecret(sess.User.ID, secret); err != nil {
				http.Error(w, "could not set up two-factor authentication", http.StatusInternalServerError)
				return
			}
			sess.User.TOTPSecret = secret
			renderSettingsWith(d, w, r,
				"Scan the QR code with your authenticator app, then enter the 6-digit code to turn it on.", "", http.StatusOK, nil)
		default:
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
		}
	})
}
