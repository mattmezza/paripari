package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mattmezza/paripari/internal/model"
)

// Transfer is one standing-order row of the transfer table.
type Transfer struct {
	FromUserID  int64  `json:"from_user_id"`
	From        string `json:"from"`
	To          string `json:"to"`
	ToAccountID int64  `json:"to_account_id,omitempty"`
	ToIBAN      string `json:"-"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Reference   string `json:"reference"`

	// DeltaCents is this row's amount minus the last confirmed amount for the
	// same from/to pair; New marks a row with no confirmed counterpart.
	DeltaCents int64 `json:"-"`
	New        bool  `json:"-"`
}

func (t Transfer) key() string { return fmt.Sprintf("%d|%s", t.FromUserID, t.To) }

// TransferPlan is the full monthly instruction set.
type TransferPlan struct {
	Currency string
	Rows     []Transfer
	// Changed is true when the plan differs from the last confirmed payload
	// (including rows that disappeared).
	Changed bool
	// Confirmed is false when no confirmation has ever been recorded.
	Confirmed bool
}

// BuildTransfers produces the consolidated transfer instructions: common
// expenses are split by the household method, and each expense's share is sent
// to its budget account — or, when it names none, to the common checking
// account (common savings for savings-tagged ones). Each partner gets one row
// per destination account; zero-amount rows are omitted.
func BuildTransfers(in Inputs, ov MonthlyOverview) TransferPlan {
	checking := commonAccount(in, "checking", "Common checking")
	savings := commonAccount(in, "savings", "Common savings")

	groups := []*transferGroup{
		{target: checking, prefix: "Monthly contribution"},
		{target: savings, prefix: "Savings contribution"},
	}
	byKey := map[string]*transferGroup{checking.key(): groups[0], savings.key(): groups[1]}
	byID := map[int64]model.Account{}
	for _, a := range in.Accounts {
		byID[a.ID] = a
	}

	for _, e := range in.Expenses {
		if e.Category != "common" {
			continue
		}
		g := groups[0]
		if e.IsSavings {
			g = groups[1]
		}
		if e.AccountID != nil {
			if a, ok := byID[*e.AccountID]; ok {
				t := target{id: a.ID, name: a.Name, iban: a.IBAN}
				if existing, ok := byKey[t.key()]; ok {
					g = existing
				} else {
					g = &transferGroup{target: t, prefix: "Budget contribution"}
					byKey[t.key()] = g
					groups = append(groups, g)
				}
			}
		}
		g.totalCents += in.amount(e.AmountCents, e.Currency)
		g.addLabel(e)
	}

	ratio := ov.Ratio
	p := TransferPlan{Currency: ov.Currency}
	for _, c := range []PartnerCashflow{ov.A, ov.B} {
		for _, g := range groups {
			a, b := ShareOf(g.totalCents, ratio)
			cents := a
			if c.UserID == ov.B.UserID {
				cents = b
			}
			if cents == 0 {
				continue
			}
			p.Rows = append(p.Rows, Transfer{
				FromUserID: c.UserID, From: c.Name + " personal",
				To: g.target.name, ToAccountID: g.target.id, ToIBAN: g.target.iban,
				AmountCents: cents, Currency: ov.Currency, Reference: g.reference(),
			})
		}
	}
	return p
}

// transferGroup is everything feeding one destination account.
type transferGroup struct {
	target     target
	prefix     string
	totalCents int64
	labels     []string
}

// addLabel records what feeds this group — the expense's subcategory when it
// has one, its name otherwise — deduplicated, in first-seen order.
func (g *transferGroup) addLabel(e model.Expense) {
	l := e.Subcategory
	if l == "" {
		l = e.Name
	}
	if l == "" {
		return
	}
	for _, existing := range g.labels {
		if existing == l {
			return
		}
	}
	g.labels = append(g.labels, l)
}

// reference is the standing-order reference: what the row is, plus what feeds
// it, e.g. "Monthly contribution · rent, utilities, insurance".
func (g *transferGroup) reference() string {
	labels := g.labels
	if len(labels) > 3 {
		labels = append(append([]string{}, labels[:3]...), "…")
	}
	if len(labels) == 0 {
		return g.prefix
	}
	return g.prefix + " · " + strings.Join(labels, ", ")
}

type target struct {
	id   int64
	name string
	iban string
}

// key identifies a destination: by account id, or by name for the fallback
// targets that have no account row behind them.
func (t target) key() string { return fmt.Sprintf("%d|%s", t.id, t.name) }

// commonAccount picks the household-level (non-personal) account with the given
// purpose, falling back to a name-only target when none exists.
func commonAccount(in Inputs, purpose, fallback string) target {
	for _, a := range in.Accounts {
		if a.UserID == nil && a.Purpose == purpose {
			return target{id: a.ID, name: a.Name, iban: a.IBAN}
		}
	}
	return target{name: fallback}
}

// Payload serialises the plan for storage in transfer_confirmations.
func (p TransferPlan) Payload() (string, error) {
	b, err := json.Marshal(p.Rows)
	return string(b), err
}

// Diff compares the plan against the last confirmed payload, filling in each
// row's DeltaCents/New and the plan's Changed flag. An empty payload means
// nothing was ever confirmed: rows are left unflagged and Changed is false.
func (p *TransferPlan) Diff(lastConfirmedPayload string) {
	if lastConfirmedPayload == "" {
		return
	}
	var prev []Transfer
	if json.Unmarshal([]byte(lastConfirmedPayload), &prev) != nil {
		return
	}
	p.Confirmed = true
	old := make(map[string]int64, len(prev))
	for _, t := range prev {
		old[t.key()] += t.AmountCents
	}
	for i := range p.Rows {
		k := p.Rows[i].key()
		amt, ok := old[k]
		if !ok {
			p.Rows[i].New = true
			p.Rows[i].DeltaCents = p.Rows[i].AmountCents
			p.Changed = true
			continue
		}
		p.Rows[i].DeltaCents = p.Rows[i].AmountCents - amt
		if p.Rows[i].DeltaCents != 0 {
			p.Changed = true
		}
		delete(old, k)
	}
	if len(old) > 0 { // rows that vanished
		p.Changed = true
	}
}
