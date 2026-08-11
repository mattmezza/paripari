package view

import (
	"net/http"
	"net/url"
)

const flashCookie = "pp_flash"

// SetFlash stores a one-shot message shown on the next rendered page.
func SetFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: url.QueryEscape(msg), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

// TakeFlash reads the flash message (if any) and, when w is non-nil, clears it.
func TakeFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(flashCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	if w != nil {
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1})
	}
	msg, _ := url.QueryUnescape(c.Value)
	return msg
}
