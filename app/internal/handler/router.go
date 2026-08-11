// Package handler wires HTTP routes. Handlers stay thin: parse, call store,
// render.
package handler

import (
	"io/fs"
	"net/http"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/store"
	"github.com/mattmezza/paripari/internal/view"
)

// GoldProvider returns the current gold spot price. The gold agent supplies the
// real implementation.
type GoldProvider interface {
	// PricePerGramCents is the 24K spot price per gram in the given currency.
	PricePerGramCents(currency string) (int64, error)
}

// Deps is what every handler section gets. Fields are interfaces or concrete
// types owned by the foundation; other agents implement against them.
type Deps struct {
	Store   *store.Store
	View    *view.View
	Auth    *auth.Manager
	Rates   view.RateProvider
	Gold    GoldProvider
	BaseURL string
	Static  fs.FS // rooted at app/static
}

// New returns the complete application handler.
func New(d *Deps) http.Handler {
	app := http.NewServeMux()
	registerDashboard(app, d)
	registerIncome(app, d)
	registerExpenses(app, d)
	registerAccounts(app, d)
	registerTransfers(app, d)
	registerNetWorth(app, d)
	registerGold(app, d)
	registerGoals(app, d)
	registerScenarios(app, d)
	registerTrips(app, d)
	registerProjections(app, d)
	registerSettings(app, d)

	root := http.NewServeMux()
	registerAuth(root, d)
	registerPublic(root, d)
	root.Handle("/", d.Auth.RequireAuth(app))
	return root
}

func registerPublic(mux *http.ServeMux, d *Deps) {
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(d.Static))))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := d.Store.DB.Ping(); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("User-agent: *\nAllow: /login\nAllow: /signup\nDisallow: /\nSitemap: " +
			d.BaseURL + "/sitemap.xml\n"))
	})

	mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
			`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
			`<url><loc>` + d.BaseURL + `/login</loc></url>` +
			`<url><loc>` + d.BaseURL + `/signup</loc></url>` +
			`</urlset>`))
	})
}
