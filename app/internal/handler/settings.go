package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/fx"
	"github.com/mattmezza/paripari/internal/gold"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/view"
)

type rateRow struct {
	model.FXRate
	Age string
}

// sessionExpiryKey maps the user's session-lifetime preference to the value
// of the settings select.
func sessionExpiryKey(days *int) string {
	if days == nil {
		return "never"
	}
	return strconv.Itoa(*days)
}

// parseSessionExpiry reads the settings select; nil means "never expire".
func parseSessionExpiry(v string) (*int, error) {
	if v == "never" || v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || (n != 7 && n != 30 && n != 90 && n != 365) {
		return nil, fmt.Errorf("Pick a valid session lifetime.")
	}
	return &n, nil
}

// settingsData backs both the full page and each swappable section.
type settingsData struct {
	CSRF               string
	Household          model.Household
	PartnerA, PartnerB model.User
	JoinStatus         string
	Currencies         []string

	Rates            []rateRow
	GoldPrice        *model.GoldPrice
	GoldAge          string
	GoldManualInput  string // "" = no override
	EffectiveGoldPPG int64  // what the app is actually using right now

	// SessionExpiryKey is the current user's session lifetime as the select's
	// value: "7", "30", "90", "365" or "never".
	SessionExpiryKey string
	TwoFactor        *twoFactorState
	StepUp           *stepUpData // non-nil renders the step-up form instead of the account card

	Notice string // transient inline confirmation, set only on the response to a mutation
	Err    string
}

func ageLabel(iso string) string {
	t, err := time.Parse("2006-01-02T15:04:05Z", iso)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func buildSettingsData(d *Deps, r *http.Request) (*settingsData, error) {
	sess := auth.FromContext(r)
	hh, err := d.Store.Household(sess.Household.ID)
	if err != nil {
		return nil, err
	}

	data := &settingsData{CSRF: sess.Token, Household: *hh, Currencies: view.Currencies, JoinStatus: "1/2"}
	if sess.User != nil {
		data.PartnerA = *sess.User
		data.SessionExpiryKey = sessionExpiryKey(sess.User.SessionExpiryDays)
		tf := &twoFactorState{Enrolled: sess.User.TOTPEnabled, Remaining: len(sess.User.RecoveryCodes)}
		if !sess.User.TOTPEnabled && sess.User.TOTPSecret != "" {
			// Mid-enrollment: the secret exists but no code has activated it yet.
			tf.Pending = true
			tf.Secret = sess.User.TOTPSecret
			if uri, err := auth.TOTPURI(sess.User.Email, sess.User.TOTPSecret); err == nil {
				tf.QRData, _ = auth.TOTPQRDataURI(uri)
			}
		}
		data.TwoFactor = tf
	}
	if sess.Partner != nil {
		data.PartnerB = *sess.Partner
		data.JoinStatus = "2/2"
	}

	rates, err := d.Store.Rates()
	if err != nil {
		return nil, err
	}
	for _, rt := range rates {
		data.Rates = append(data.Rates, rateRow{FXRate: rt, Age: ageLabel(rt.FetchedAt)})
	}

	if gp, err := d.Store.LatestGoldPrice(); err == nil {
		data.GoldPrice = gp
		data.GoldAge = ageLabel(gp.FetchedAt)
	}
	if hh.ManualGoldPriceCents != nil {
		data.GoldManualInput = plainAmount(*hh.ManualGoldPriceCents)
	}
	if d.Gold != nil {
		data.EffectiveGoldPPG, _ = d.Gold.PricePerGramCents(hh.ID, hh.DisplayCurrency)
	}
	return data, nil
}

func registerSettings(mux *http.ServeMux, d *Deps) {
	render := func(w http.ResponseWriter, r *http.Request, notice, errMsg string, status int) {
		data, err := buildSettingsData(d, r)
		if err != nil {
			http.Error(w, "could not load settings", http.StatusInternalServerError)
			return
		}
		data.Notice, data.Err = notice, errMsg
		d.View.RenderStatus(w, r, status, "settings", &view.PageData{Title: "Settings", Data: data})
	}
	partial := func(w http.ResponseWriter, r *http.Request, name, notice, errMsg string, status int) {
		data, err := buildSettingsData(d, r)
		if err != nil {
			http.Error(w, "could not load settings", http.StatusInternalServerError)
			return
		}
		data.Notice, data.Err = notice, errMsg
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		d.View.Partial(w, "partials/"+name, data)
	}

	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "", "", http.StatusOK)
	})

	registerBackup(mux, d, partial)
	registerSecurity(mux, d, partial)

	mux.HandleFunc("POST /settings/household", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		hh, err := d.Store.Household(sess.Household.ID)
		if err != nil {
			http.Error(w, "could not load household", http.StatusInternalServerError)
			return
		}
		splitMethod := r.PostFormValue("split_method")
		currency := r.PostFormValue("display_currency")
		if splitMethod != "fifty_fifty" && splitMethod != "income_weighted" {
			partial(w, r, "settings-household", "", "Pick a valid split method.", http.StatusUnprocessableEntity)
			return
		}
		// The radio only renders for income_weighted, so an absent value means
		// "net" rather than a bad request.
		basis := r.PostFormValue("weight_basis")
		if basis != "gross" {
			basis = "net"
		}
		if !ValidCurrency(currency) {
			partial(w, r, "settings-household", "", "Pick a supported display currency.", http.StatusUnprocessableEntity)
			return
		}
		hh.SplitMethod = splitMethod
		hh.WeightBasis = basis
		hh.IncludeVariableIncome = r.PostFormValue("include_variable_income") == "on"
		hh.DisplayCurrency = currency
		if err := d.Store.UpdateHouseholdSettings(hh); err != nil {
			http.Error(w, "could not save settings", http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		partial(w, r, "settings-household", "Saved.", "", http.StatusOK)
	})

	mux.HandleFunc("POST /settings/gold", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		hh, err := d.Store.Household(sess.Household.ID)
		if err != nil {
			http.Error(w, "could not load household", http.StatusInternalServerError)
			return
		}
		raw := strings.TrimSpace(r.PostFormValue("manual_price"))
		if raw == "" {
			hh.ManualGoldPriceCents = nil
		} else {
			cents, err := ParseAmount(raw)
			if err != nil {
				partial(w, r, "settings-data", "", "That gold price doesn't look valid.", http.StatusUnprocessableEntity)
				return
			}
			hh.ManualGoldPriceCents = &cents
		}
		if err := d.Store.UpdateHouseholdSettings(hh); err != nil {
			http.Error(w, "could not save settings", http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		partial(w, r, "settings-data", "Saved.", "", http.StatusOK)
	})

	mux.HandleFunc("POST /settings/fx/refresh", func(w http.ResponseWriter, r *http.Request) {
		notice, errMsg := "Rates refreshed.", ""
		// fx.Provider (Deps.Rates) only exposes Convert; ForceRefresh lives on the
		// separate fx.Refresher used by the daily job. It's a thin wrapper around
		// *store.Store, cheap to construct here rather than widen Deps for it.
		if err := fx.NewRefresher(d.Store).ForceRefresh(r.Context()); err != nil {
			notice, errMsg = "", "Couldn't reach the FX provider. Showing the last cached rates."
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		partial(w, r, "settings-data", notice, errMsg, http.StatusOK)
	})

	mux.HandleFunc("POST /settings/gold/refresh", func(w http.ResponseWriter, r *http.Request) {
		notice, errMsg := "Gold spot price refreshed.", ""
		if err := gold.NewRefresher(d.Store).ForceRefresh(r.Context()); err != nil {
			notice, errMsg = "", "Couldn't reach the gold price provider. Showing the last cached price."
		}
		w.Header().Set("HX-Trigger", "pp:recalc")
		partial(w, r, "settings-data", notice, errMsg, http.StatusOK)
	})

	// Session lifetime: per-user, applies to the current session immediately.
	mux.HandleFunc("POST /settings/session-expiry", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		days, err := parseSessionExpiry(r.PostFormValue("session_expiry_days"))
		if err != nil {
			partial(w, r, "settings-security", "", err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if err := d.Store.UpdateUserSessionExpiry(sess.User.ID, days); err != nil {
			http.Error(w, "could not save settings", http.StatusInternalServerError)
			return
		}
		// Apply to the live session right away, not on next login.
		sess.User.SessionExpiryDays = days
		if err := d.Auth.RefreshSession(w, sess.Token, sess.User); err != nil {
			http.Error(w, "could not save settings", http.StatusInternalServerError)
			return
		}
		partial(w, r, "settings-security", "Saved. Your current session now follows this lifetime.", "", http.StatusOK)
	})

	mux.HandleFunc("POST /settings/password", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		// Password changes are sensitive: require a fresh step-up first.
		if auth.RequiresStepUp(sess) {
			renderStepUp(d, w, r, "password", "", http.StatusOK)
			return
		}
		current, next := r.PostFormValue("current_password"), r.PostFormValue("new_password")
		if sess.User == nil || !auth.VerifyPassword(sess.User.PasswordHash, current) {
			render(w, r, "", "Current password is wrong.", http.StatusUnprocessableEntity)
			return
		}
		if len(next) < 8 {
			render(w, r, "", "New password must be at least 8 characters.", http.StatusUnprocessableEntity)
			return
		}
		hash, err := auth.HashPassword(next)
		if err != nil {
			http.Error(w, "could not set password", http.StatusInternalServerError)
			return
		}
		if err := d.Store.UpdateUserPassword(sess.User.ID, hash); err != nil {
			http.Error(w, "could not set password", http.StatusInternalServerError)
			return
		}
		// The current session stays; every other session for this user is dead.
		if err := d.Store.DeleteOtherSessions(sess.Token, sess.User.ID); err != nil {
			http.Error(w, "could not revoke other sessions", http.StatusInternalServerError)
			return
		}
		view.SetFlash(w, "Password updated. Other devices were signed out.")
		redirect(w, r, "/settings")
	})

	// Danger zone: no delete-household in v1 — a self-hosted single-household
	// instance has nothing sensible to "delete" into. Skipped.
}
