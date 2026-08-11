package view

import (
	"encoding/json"
	"fmt"
	"html/template"
	"reflect"
	"strconv"
	"time"

	"github.com/mattmezza/paripari/internal/service"
)

// funcs is the parse-time FuncMap. moneyIn is rebound per request (see
// requestFuncs) so it knows the household display currency; the default here
// converts into CHF.
func (v *View) funcs() template.FuncMap {
	return template.FuncMap{
		"money":     Money,
		"amount":    service.Amount,
		"pct100":    pct100,
		"dedYearly": service.DeductionYearly,
		"moneyIn":   v.moneyInto("CHF"),
		"pct":       Pct,
		"date":      Date,
		"dict":      dict,
		"json":      toJSON,
		"kindIs":    kindIs,
	}
}

// pct100 renders hundredths of a percent: 530 -> "5.3%".
func pct100(hundredths int64) string {
	s := strconv.FormatFloat(float64(hundredths)/100, 'f', -1, 64)
	return s + "%"
}

// kindIs reports whether v's reflect kind matches (e.g. `kindIs "string" .`).
func kindIs(kind string, v any) bool {
	if v == nil {
		return kind == "invalid"
	}
	return reflect.ValueOf(v).Kind().String() == kind
}

// requestFuncs rebinds the currency-aware funcs for one request.
func (v *View) requestFuncs(displayCurrency string) template.FuncMap {
	return template.FuncMap{"moneyIn": v.moneyInto(displayCurrency)}
}

// moneyInto returns a template func `moneyIn cents fromCur` converting into to.
func (v *View) moneyInto(to string) func(int64, string) string {
	return func(cents int64, from string) string { return v.moneyIn(cents, from, to) }
}

// Money formats minor units in the given currency. Thin wrapper: the formatter
// lives in the engine package (service.Money) so calculations and templates
// agree on presentation.
func Money(cents int64, currency string) string { return service.Money(cents, currency) }

// moneyIn converts an amount from its native currency into the household
// display currency and formats it.
func (v *View) moneyIn(cents int64, from, to string) string {
	rates := v.Rates
	if rates == nil {
		rates = Identity
	}
	return Money(rates.Convert(cents, from, to), to)
}

// Pct formats a ratio (0.4449) as a percentage string (44.5%).
func Pct(ratio float64) string { return strconv.FormatFloat(ratio*100, 'f', 1, 64) + "%" }

// Date reformats an ISO8601 date/timestamp as 02 Jan 2006. Unparseable input is
// returned unchanged.
func Date(s string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("02 Jan 2006")
		}
	}
	return s
}

func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is not a string", i)
		}
		m[k] = kv[i+1]
	}
	return m, nil
}

func toJSON(v any) (template.JS, error) {
	b, err := json.Marshal(v)
	return template.JS(b), err
}
