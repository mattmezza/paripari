package service

import (
	"encoding/json"
	"fmt"
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

// BuildTransfers produces the consolidated transfer instructions: per partner,
// one transfer to the common checking account for their share of ordinary
// common expenses and one to the common savings account for their share of
// savings-tagged common expenses. Zero-amount rows are omitted.
//
// ponytail: expenses carry no account linkage in the schema, so grouping is by
// savings-vs-not, which is exactly prompt.md's example table. If expenses ever
// gain an account_id, group by it here.
func BuildTransfers(in Inputs, ov MonthlyOverview) TransferPlan {
	checking := commonAccount(in, "checking", "Common checking")
	savings := commonAccount(in, "savings", "Common savings")

	p := TransferPlan{Currency: ov.Currency}
	for _, c := range []PartnerCashflow{ov.A, ov.B} {
		add := func(to target, cents int64, ref string) {
			if cents == 0 {
				return
			}
			p.Rows = append(p.Rows, Transfer{
				FromUserID: c.UserID, From: c.Name + " personal",
				To: to.name, ToAccountID: to.id, ToIBAN: to.iban,
				AmountCents: cents, Currency: ov.Currency, Reference: ref,
			})
		}
		add(checking, c.CommonShareCents-c.CommonSavingsShareCents, "Monthly contribution")
		add(savings, c.CommonSavingsShareCents, "Savings contribution")
	}
	return p
}

type target struct {
	id   int64
	name string
	iban string
}

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
