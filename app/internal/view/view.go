// Package view owns the HTML render contract: full pages inside the base
// layout, bare content for htmx swaps, and explicit partials.
package view

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/mattmezza/paripari/internal/model"
)

// RateProvider converts money between currencies. The fx agent supplies the
// real implementation; Identity is used until then.
type RateProvider interface {
	// Convert returns amountCents expressed in `to`, rounded to whole cents.
	Convert(amountCents int64, from, to string) int64
}

type identityRates struct{}

func (identityRates) Convert(c int64, _, _ string) int64 { return c }

// Identity is a 1:1 RateProvider placeholder.
var Identity RateProvider = identityRates{}

// Currencies is the supported display-currency list.
var Currencies = []string{"CHF", "EUR", "USD", "GBP", "TRY"}

// PageData is available as the dot in every template.
type PageData struct {
	Title            string
	Description      string
	Path             string
	BaseURL          string
	CanonicalURL     string
	User             *model.User
	Partner          *model.User
	Household        *model.Household
	DisplayCurrency  string
	Currencies       []string
	CSRF             string
	Flash            string
	TransfersChanged bool
	Data             any
}

// ContextData is what the auth middleware makes available per request; view
// reads it to fill PageData. Handlers may also build one by hand.
type ContextData struct {
	User      *model.User
	Partner   *model.User
	Household *model.Household
	CSRF      string
}

// ContextFunc extracts per-request user/household data. Wired to auth.FromContext in main.
type ContextFunc func(r *http.Request) ContextData

type View struct {
	fsys    fs.FS // rooted at app/templates
	BaseURL string
	Dev     bool
	Rates   RateProvider
	Context ContextFunc
	// TransfersChanged reports whether transfer amounts differ from the last
	// confirmed set. Optional; nil means "no flag".
	TransfersChanged func(householdID int64) bool

	pages    map[string]*template.Template
	partials *template.Template
}

// New parses templates from fsys (an FS rooted at app/templates).
func New(fsys fs.FS, baseURL string, dev bool) (*View, error) {
	v := &View{fsys: fsys, BaseURL: baseURL, Dev: dev, Rates: Identity}
	return v, v.parse()
}

func (v *View) parse() error {
	pageFiles, err := fs.Glob(v.fsys, "*.html")
	if err != nil {
		return err
	}
	partialFiles, _ := fs.Glob(v.fsys, "partials/*.html")
	layoutFiles, err := fs.Glob(v.fsys, "layouts/*.html")
	if err != nil {
		return err
	}
	if len(layoutFiles) == 0 {
		return fmt.Errorf("view: no layouts found")
	}

	pages := map[string]*template.Template{}
	for _, pf := range pageFiles {
		name := strings.TrimSuffix(path.Base(pf), ".html")
		files := append(append([]string{}, layoutFiles...), partialFiles...)
		files = append(files, pf)
		t, err := template.New("base.html").Funcs(v.funcs()).ParseFS(v.fsys, files...)
		if err != nil {
			return fmt.Errorf("view: parse %s: %w", pf, err)
		}
		pages[name] = t
	}
	v.pages = pages

	if len(partialFiles) > 0 {
		p, err := template.New("partials").Funcs(v.funcs()).ParseFS(v.fsys, partialFiles...)
		if err != nil {
			return fmt.Errorf("view: parse partials: %w", err)
		}
		v.partials = p
	}
	return nil
}

func (v *View) reloadIfDev() error {
	if v.Dev {
		return v.parse()
	}
	return nil
}

// Render writes page `name` (app/templates/<name>.html). With an HX-Request
// header only the page's "content" block is written; otherwise the full layout.
func (v *View) Render(w http.ResponseWriter, r *http.Request, name string, data any) {
	v.RenderStatus(w, r, http.StatusOK, name, data)
}

func (v *View) RenderStatus(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	if err := v.reloadIfDev(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t, ok := v.pages[name]
	if !ok {
		http.Error(w, "view: unknown page "+name, http.StatusInternalServerError)
		return
	}
	pd := v.PageData(r, data)
	if pd.Flash != "" {
		TakeFlash(w, r) // clear the cookie now that it is being shown
	}
	// hx-boost navigations must get the whole document (htmx swaps the body and
	// picks up the title); only explicit hx-get requests get the bare content.
	root := "base"
	if t.Lookup(root) == nil {
		root = "base.html"
	}
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true" {
		root = "content"
	}
	t, err := t.Clone()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t = t.Funcs(v.requestFuncs(pd.DisplayCurrency))
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, root, pd); err != nil {
		http.Error(w, "view: render "+name+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

// Partial renders a fragment, e.g. Partial(w, "partials/transfer-table", data).
// `data` is used as the dot as-is (no PageData wrapping).
func (v *View) Partial(w http.ResponseWriter, name string, data any) {
	if err := v.reloadIfDev(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if v.partials == nil {
		http.Error(w, "view: no partials", http.StatusInternalServerError)
		return
	}
	base := strings.TrimSuffix(path.Base(name), ".html") + ".html"
	var buf bytes.Buffer
	if err := v.partials.ExecuteTemplate(&buf, base, data); err != nil {
		http.Error(w, "view: partial "+name+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// PageData assembles the standard dot for a request.
func (v *View) PageData(r *http.Request, data any) PageData {
	var c ContextData
	if v.Context != nil {
		c = v.Context(r)
	}
	pd := PageData{
		Title:           "PariPari",
		Description:     "Plan your household finances together, on equal terms.",
		Path:            r.URL.Path,
		BaseURL:         v.BaseURL,
		CanonicalURL:    strings.TrimRight(v.BaseURL, "/") + r.URL.Path,
		User:            c.User,
		Partner:         c.Partner,
		Household:       c.Household,
		DisplayCurrency: "CHF",
		Currencies:      Currencies,
		CSRF:            c.CSRF,
		Flash:           TakeFlash(nil, r),
		Data:            data,
	}
	if c.Household != nil {
		pd.DisplayCurrency = c.Household.DisplayCurrency
		if v.TransfersChanged != nil {
			pd.TransfersChanged = v.TransfersChanged(c.Household.ID)
		}
	}
	// A page-supplied *PageData overrides title/description etc.
	if over, ok := data.(*PageData); ok && over != nil {
		if over.Title != "" {
			pd.Title = over.Title
		}
		if over.Description != "" {
			pd.Description = over.Description
		}
		pd.Data = over.Data
	}
	return pd
}
