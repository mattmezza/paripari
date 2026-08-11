package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// BuildInputs assembles service.Inputs for the authenticated household from
// the store. Shared across screens (dashboard, income, expenses, ...) so the
// engine always sees the same shape.
func BuildInputs(d *Deps, r *http.Request) (service.Inputs, error) {
	sess := auth.FromContext(r)
	if sess == nil {
		return service.Inputs{}, fmt.Errorf("no session")
	}
	hh := sess.Household
	partnerA, partnerB := *sess.User, model.User{}
	if sess.Partner != nil {
		partnerB = *sess.Partner
	}
	// PartnerA is the lower user id by convention (zero-value B, no partner
	// yet, always sorts last).
	if partnerB.ID != 0 && partnerB.ID < partnerA.ID {
		partnerA, partnerB = partnerB, partnerA
	}

	incomes, err := d.Store.Incomes(hh.ID)
	if err != nil {
		return service.Inputs{}, err
	}
	expenses, err := d.Store.Expenses(hh.ID)
	if err != nil {
		return service.Inputs{}, err
	}
	accounts, err := d.Store.Accounts(hh.ID)
	if err != nil {
		return service.Inputs{}, err
	}
	assets, err := d.Store.Assets(hh.ID)
	if err != nil {
		return service.Inputs{}, err
	}
	gold, err := d.Store.GoldItems(hh.ID)
	if err != nil {
		return service.Inputs{}, err
	}
	goals, err := d.Store.Goals(hh.ID)
	if err != nil {
		return service.Inputs{}, err
	}

	var goldPrice int64
	if d.Gold != nil {
		if p, err := d.Gold.PricePerGramCents(hh.ID, hh.DisplayCurrency); err == nil {
			goldPrice = p
		}
	}

	return service.Inputs{
		Household:             *hh,
		PartnerA:              partnerA,
		PartnerB:              partnerB,
		Incomes:               incomes,
		Expenses:              expenses,
		Accounts:              accounts,
		Assets:                assets,
		Gold:                  gold,
		Goals:                 goals,
		Rates:                 d.Rates,
		Display:               hh.DisplayCurrency,
		GoldPricePerGramCents: goldPrice,
		AnnualReturnRate:      0.05,
	}, nil
}

// ParseAmount parses a decimal amount string into integer cents. Accepts
// "1'234.50", "1'234,50", "1234,50" and "1234.50" — apostrophes are thousands
// separators; the last '.' or ',' in the string is the decimal point.
func ParseAmount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("amount is required")
	}
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, " ", "")
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	dot, comma := strings.LastIndexByte(s, '.'), strings.LastIndexByte(s, ',')
	dec := dot
	if comma > dec {
		dec = comma
	}
	intPart, fracPart := s, "00"
	if dec >= 0 {
		intPart, fracPart = s[:dec], s[dec+1:]
		intPart = strings.NewReplacer(".", "", ",", "").Replace(intPart)
	}
	if len(fracPart) == 0 {
		fracPart = "00"
	} else if len(fracPart) == 1 {
		fracPart += "0"
	} else if len(fracPart) > 2 {
		fracPart = fracPart[:2]
	}
	if intPart == "" {
		intPart = "0"
	}
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not a valid amount")
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not a valid amount")
	}
	cents := whole*100 + frac
	if neg {
		cents = -cents
	}
	return cents, nil
}

// CentsToStr renders integer cents as a plain decimal string suitable for a
// form input's value — no currency code, no thousands separator, e.g.
// 123456 -> "1234.56". (service.Money is for display, not editing.)
func CentsToStr(cents int64) string {
	neg := ""
	if cents < 0 {
		neg = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", neg, cents/100, cents%100)
}

// ValidCurrency reports whether c is a supported display/native currency.
func ValidCurrency(c string) bool {
	for _, cur := range view.Currencies {
		if cur == c {
			return true
		}
	}
	return false
}
