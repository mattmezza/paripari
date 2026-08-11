package service

import "testing"

func planOf(in Inputs) TransferPlan { return BuildTransfers(in, BuildOverview(in)) }

func TestBuildTransfers(t *testing.T) {
	p := planOf(baseInputs())
	if len(p.Rows) != 4 {
		t.Fatalf("rows = %d, want 4: %+v", len(p.Rows), p.Rows)
	}
	want := []Transfer{
		{FromUserID: 1, From: "Ada personal", To: "Common checking", ToAccountID: 30, AmountCents: 133423, Currency: "CHF", Reference: "Monthly contribution"},
		{FromUserID: 1, From: "Ada personal", To: "Common savings", ToAccountID: 31, AmountCents: 44474, Currency: "CHF", Reference: "Savings contribution"},
		{FromUserID: 2, From: "Bo personal", To: "Common checking", ToAccountID: 30, AmountCents: 166577, Currency: "CHF", Reference: "Monthly contribution"},
		{FromUserID: 2, From: "Bo personal", To: "Common savings", ToAccountID: 31, AmountCents: 55526, Currency: "CHF", Reference: "Savings contribution"},
	}
	for i, w := range want {
		g := p.Rows[i]
		if g.FromUserID != w.FromUserID || g.From != w.From || g.To != w.To ||
			g.ToAccountID != w.ToAccountID || g.AmountCents != w.AmountCents || g.Reference != w.Reference {
			t.Errorf("row %d = %+v, want %+v", i, g, w)
		}
	}
	if p.Rows[1].ToIBAN != "CH99" {
		t.Errorf("IBAN not carried through: %q", p.Rows[1].ToIBAN)
	}
	// Transfers must cover exactly the common expenses.
	var total int64
	for _, r := range p.Rows {
		total += r.AmountCents
	}
	if total != 400000 {
		t.Errorf("transfers total = %d, want 400000", total)
	}
}

func TestBuildTransfersFallbackAccountsAndZeroRows(t *testing.T) {
	in := baseInputs()
	in.Accounts = nil             // no common accounts configured
	in.Expenses = in.Expenses[:1] // rent only, no savings
	p := planOf(in)
	if len(p.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (no savings rows)", len(p.Rows))
	}
	if p.Rows[0].To != "Common checking" || p.Rows[0].ToAccountID != 0 {
		t.Errorf("fallback target = %+v", p.Rows[0])
	}
}

func TestTransferDiff(t *testing.T) {
	base := planOf(baseInputs())
	payload, err := base.Payload()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("never confirmed", func(t *testing.T) {
		p := planOf(baseInputs())
		p.Diff("")
		if p.Changed || p.Confirmed {
			t.Errorf("empty payload must not flag a change: %+v", p)
		}
	})

	t.Run("unchanged", func(t *testing.T) {
		p := planOf(baseInputs())
		p.Diff(payload)
		if p.Changed {
			t.Errorf("identical plan must not be flagged changed")
		}
		if !p.Confirmed {
			t.Errorf("Confirmed should be true")
		}
		for _, r := range p.Rows {
			if r.DeltaCents != 0 || r.New {
				t.Errorf("row %+v should have no delta", r)
			}
		}
	})

	t.Run("amount changed", func(t *testing.T) {
		in := baseInputs()
		in.Expenses[0].AmountCents = 350000 // rent +500
		p := planOf(in)
		p.Diff(payload)
		if !p.Changed {
			t.Fatal("expected Changed")
		}
		var deltaSum int64
		for _, r := range p.Rows {
			deltaSum += r.DeltaCents
		}
		if deltaSum != 50000 {
			t.Errorf("sum of deltas = %d, want 50000", deltaSum)
		}
	})

	t.Run("row removed", func(t *testing.T) {
		in := baseInputs()
		in.Expenses = in.Expenses[:1] // drop the common savings expense
		p := planOf(in)
		p.Diff(payload)
		if !p.Changed {
			t.Error("dropping a transfer row must flag a change")
		}
		if len(p.Rows) != 2 {
			t.Errorf("rows = %d, want 2", len(p.Rows))
		}
	})

	t.Run("row added", func(t *testing.T) {
		short := planOf(baseInputs())
		short.Rows = short.Rows[:2]
		shortPayload, _ := short.Payload()
		p := planOf(baseInputs())
		p.Diff(shortPayload)
		if !p.Changed {
			t.Fatal("expected Changed")
		}
		if !p.Rows[2].New || p.Rows[2].DeltaCents != p.Rows[2].AmountCents {
			t.Errorf("row 2 should be new: %+v", p.Rows[2])
		}
	})

	t.Run("garbage payload is ignored", func(t *testing.T) {
		p := planOf(baseInputs())
		p.Diff("not json")
		if p.Changed || p.Confirmed {
			t.Errorf("garbage payload must be a no-op")
		}
	})
}
