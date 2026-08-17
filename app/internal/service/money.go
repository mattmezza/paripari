// Package service is the PariPari calculation engine: pure, deterministic
// functions over the domain model. Nothing here fetches, reads a clock, or
// touches the database — rates, prices and time horizons are parameters.
//
// All money is INTEGER minor units (cents) in the amount's native currency
// until an explicit conversion through a Rates provider.
package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Rates converts money between currencies. view.RateProvider satisfies it
// structurally, so handlers can pass Deps.Rates straight in.
type Rates interface {
	// Convert returns amountCents expressed in `to`, rounded to whole cents.
	Convert(amountCents int64, from, to string) int64
}

type identityRates struct{}

func (identityRates) Convert(c int64, _, _ string) int64 { return c }

// Identity is a 1:1 Rates placeholder (also the nil fallback everywhere here).
var Identity Rates = identityRates{}

func conv(r Rates, cents int64, from, to string) int64 {
	if from == to || from == "" || to == "" {
		return cents
	}
	if r == nil {
		return cents
	}
	return r.Convert(cents, from, to)
}

// Convert expresses amountCents (in `from`) in `to` through r, identity when r
// is nil or the currencies are equal/empty. Exported form of conv, for callers
// (handlers) that must convert a single amount with the engine's own rules.
func Convert(r Rates, cents int64, from, to string) int64 {
	return conv(r, cents, from, to)
}

// Amount formats minor units like Money but without the currency prefix, for
// UI spots that render the currency separately (e.g. a muted .figure__cur span).
func Amount(cents int64, currency string) string {
	return strings.Replace(Money(cents, currency), currency+" ", "", 1)
}

// Money formats minor units in the given currency: CHF uses apostrophe
// thousands separators (CHF 1'234.50), everything else a comma.
func Money(cents int64, currency string) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	sep := ","
	if currency == "CHF" {
		sep = "'"
	}
	s := group(strconv.FormatInt(cents/100, 10), sep)
	out := fmt.Sprintf("%s %s.%02d", currency, s, cents%100)
	if neg {
		out = "-" + out
	}
	return out
}

func group(digits, sep string) string {
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(c)
	}
	return b.String()
}

// divRound divides two integers rounding half away from zero.
func divRound(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	q, r := a/b, a%b
	if 2*abs(r) >= abs(b) {
		if (a < 0) != (b < 0) {
			q--
		} else {
			q++
		}
	}
	return q
}

// scale multiplies cents by a float factor, rounding half away from zero.
func scale(cents int64, f float64) int64 {
	return int64(math.Round(float64(cents) * f))
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
