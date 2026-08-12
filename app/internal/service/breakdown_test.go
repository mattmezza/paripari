package service

import (
	"testing"

	"github.com/mattmezza/paripari/internal/model"
)

// breakdownInputs relabels the base fixture's expenses so grouping has something
// to group by, and adds a second common row in a foreign currency.
func breakdownInputs() Inputs {
	in := baseInputs()
	in.Expenses[0].Subcategory = "housing" // 3'000 common rent
	in.Expenses[1].Subcategory = "savings" // 1'000 common savings
	in.Expenses[2].Subcategory = "gym"     // 500 Ada
	in.Expenses[3].Subcategory = "gym"     // 600 Bo
	in.Expenses = append(in.Expenses,
		commonExp(24, 20000, "EUR", false),     // 200 EUR -> 190 CHF, no subcategory
		personalExp(25, 1, 30000, "CHF", true), // 300 Ada, savings
	)
	in.Expenses[5].Subcategory = "" // stays Uncategorised
	return in
}

func TestBuildBreakdown_Grouping(t *testing.T) {
	b := BuildBreakdown(breakdownInputs())

	if b.Currency != "CHF" {
		t.Fatalf("currency = %q", b.Currency)
	}
	// Common: housing 3'000, savings 1'000, uncategorised 190 (EUR converted).
	want := []BreakdownRow{
		{Subcategory: "housing", AmountCents: 300000, Savings: false},
		{Subcategory: "savings", AmountCents: 100000, Savings: true},
		{Subcategory: Uncategorised, AmountCents: 19000, Savings: false},
	}
	if len(b.Common) != len(want) {
		t.Fatalf("common rows = %d, want %d: %+v", len(b.Common), len(want), b.Common)
	}
	for i, w := range want {
		got := b.Common[i]
		if got.Subcategory != w.Subcategory || got.AmountCents != w.AmountCents || got.Savings != w.Savings {
			t.Errorf("common[%d] = %+v, want %+v", i, got, w)
		}
	}

	// Personal: gym holds both partners on one row; Ada's savings row is separate.
	if len(b.Personal) != 2 {
		t.Fatalf("personal rows = %+v", b.Personal)
	}
	gym := b.Personal[0]
	if gym.Subcategory != "gym" || gym.ACents != 50000 || gym.BCents != 60000 || gym.Savings {
		t.Errorf("gym row = %+v", gym)
	}
	sav := b.Personal[1]
	if sav.Subcategory != Uncategorised || sav.ACents != 30000 || sav.BCents != 0 || !sav.Savings {
		t.Errorf("savings row = %+v", sav)
	}

	// ShareBP is basis points of household net income (8'954 + 11'179).
	if got := b.Common[0].ShareBP; got != rateBP(300000, 2013300) {
		t.Errorf("housing share = %d bp", got)
	}
}

// The sankey must conserve money: everything leaving a partner node equals
// their net income, and every intermediate node passes its inflow straight on.
func TestBuildBreakdown_SankeyBalances(t *testing.T) {
	in := breakdownInputs()
	b := BuildBreakdown(in)
	ov := BuildOverview(in)

	out, inFlow := map[string]int64{}, map[string]int64{}
	for _, f := range b.Sankey {
		if f.Cents <= 0 {
			t.Errorf("non-positive flow %+v", f)
		}
		if _, ok := b.Columns[f.From]; !ok {
			t.Errorf("flow from unpinned node %q", f.From)
		}
		if _, ok := b.Labels[f.To]; !ok {
			t.Errorf("flow to unlabelled node %q", f.To)
		}
		if b.Columns[f.To] != b.Columns[f.From]+1 {
			t.Errorf("flow skips a column: %+v (%d -> %d)", f, b.Columns[f.From], b.Columns[f.To])
		}
		out[f.From] += f.Cents
		inFlow[f.To] += f.Cents
	}

	if got := out["in:a"]; got != ov.A.NetIncomeCents {
		t.Errorf("Ada out = %d, want net income %d", got, ov.A.NetIncomeCents)
	}
	if got := out["in:b"]; got != ov.B.NetIncomeCents {
		t.Errorf("Bo out = %d, want net income %d", got, ov.B.NetIncomeCents)
	}
	// The six middle nodes pass their inflow straight on; the last column is
	// all leaves.
	for _, node := range []string{"sp:common", "sp:a", "sp:b", "kp:common", "kp:a", "kp:b"} {
		if inFlow[node] != out[node] {
			t.Errorf("node %s: in %d, out %d", node, inFlow[node], out[node])
		}
	}
	for node, col := range b.Columns {
		if col == 2 && out[node] != 0 {
			t.Errorf("last-column node %s still sends %d onward", node, out[node])
		}
	}
	// The spent branch splits common from each partner's own spending; the
	// kept branch does the same and carries no spending at all.
	if inFlow["sp:common"] != ov.CommonExpensesCents {
		t.Errorf("common spending = %d, want %d", inFlow["sp:common"], ov.CommonExpensesCents)
	}
	if inFlow["sp:a"] != ov.A.PersonalExpensesCents || inFlow["sp:b"] != ov.B.PersonalExpensesCents {
		t.Errorf("personal spending = %d / %d, want %d / %d",
			inFlow["sp:a"], inFlow["sp:b"], ov.A.PersonalExpensesCents, ov.B.PersonalExpensesCents)
	}
	if inFlow["kp:common"] != ov.CommonSavingsCents {
		t.Errorf("common kept = %d, want %d", inFlow["kp:common"], ov.CommonSavingsCents)
	}
	if out["sp:a"]+out["sp:b"] != ov.PersonalExpensesCents {
		t.Errorf("personal spending out = %d, want %d", out["sp:a"]+out["sp:b"], ov.PersonalExpensesCents)
	}
	if inFlow["k:left"] != ov.AvailableCents {
		t.Errorf("left over = %d, want %d", inFlow["k:left"], ov.AvailableCents)
	}
	// The kept rows still attribute every destination to the partners — the
	// chart stops at the destination, the table below it does not.
	var keptA, keptB int64
	for _, r := range b.Kept {
		keptA, keptB = keptA+r.ACents, keptB+r.BCents
	}
	wantOwn := func(p PartnerCashflow) int64 {
		return p.PersonalSavingsCents + p.CommonSavingsShareCents + max0(p.AvailableCents)
	}
	if keptA != wantOwn(ov.A) || keptB != wantOwn(ov.B) {
		t.Errorf("kept per partner = %d / %d, want %d / %d", keptA, keptB, wantOwn(ov.A), wantOwn(ov.B))
	}
	// Common savings sit in a savings bucket, not under the common subcategories.
	if inFlow["k:savings"] != ov.TotalSavingsCents {
		t.Errorf("savings bucket = %d, want %d", inFlow["k:savings"], ov.TotalSavingsCents)
	}
	if _, ok := b.Columns["c:savings"]; ok {
		t.Error("a savings subcategory leaked into the spending column")
	}
	for node, col := range map[string]int{
		"in:a": 0, "sp:common": 1, "sp:a": 1, "kp:common": 1, "kp:a": 1,
		"c:housing": 2, "p:gym": 2, "k:savings": 2, "k:left": 2,
	} {
		if b.Columns[node] != col {
			t.Errorf("column[%s] = %d, want %d", node, b.Columns[node], col)
		}
	}

	// Every node is drawn, ordered, and reachable: a node with a column but no
	// priority (or the other way round) is a stub the chart cannot place.
	for key := range b.Columns {
		if _, ok := b.Priority[key]; !ok {
			t.Errorf("node %s has no priority", key)
		}
		if inFlow[key] == 0 && out[key] == 0 {
			t.Errorf("node %s is declared but carries nothing", key)
		}
	}
	// The spending branch is declared above the kept one, and the partners
	// keep the same order in every column.
	if !(b.Priority["sp:common"] < b.Priority["kp:common"] && b.Priority["sp:a"] < b.Priority["sp:b"]) {
		t.Errorf("branch order is wrong: %+v", b.Priority)
	}
	if _, ok := b.Columns["own:a"]; ok {
		t.Error("the per-partner column is back")
	}
}

// Kept money splits by its tag first — an investment or a pension is not
// plain savings — and falls back to the purpose of the account it is paid
// into for rows tagged only "savings".
func TestBuildBreakdown_KeptByTag(t *testing.T) {
	in := breakdownInputs()
	in.Expenses[1].Kind = model.KindInvestment // the 1'000 common savings row

	b := BuildBreakdown(in)
	got := map[string]int64{}
	for _, r := range b.Kept {
		got[r.Purpose] = r.AmountCents
	}
	if got["investment"] != 100000 {
		t.Errorf("investment bucket = %d, want 100000 (%+v)", got["investment"], b.Kept)
	}
	if got["savings"] != 30000 {
		t.Errorf("savings bucket = %d, want Ada's 300 personal savings (%+v)", got["savings"], b.Kept)
	}
	// The tag still means "not spent", so it stays out of the spending branch.
	for _, f := range b.Sankey {
		if f.From == "b:common" && f.To == "c:savings" {
			t.Errorf("investment drawn as spending: %+v", f)
		}
	}
}

func TestBuildBreakdown_KeptByAccountPurpose(t *testing.T) {
	in := breakdownInputs()
	pension := model.Account{ID: 32, Name: "Pillar 3a", Currency: "CHF", Purpose: "pension"}
	in.Accounts = append(in.Accounts, pension)
	in.Expenses[1].AccountID = &pension.ID // the 1'000 common savings row

	b := BuildBreakdown(in)
	got := map[string]int64{}
	for _, r := range b.Kept {
		got[r.Purpose] = r.AmountCents
	}
	if got["pension"] != 100000 {
		t.Errorf("pension bucket = %d, want 100000 (%+v)", got["pension"], b.Kept)
	}
	if got["savings"] != 30000 {
		t.Errorf("savings bucket = %d, want Ada's 300 personal savings (%+v)", got["savings"], b.Kept)
	}
	for _, r := range b.Kept {
		if r.Purpose == "pension" && r.Label != "Pension" {
			t.Errorf("pension label = %q", r.Label)
		}
	}
}

// Overspending drops the "left over" ribbon rather than drawing a negative one.
func TestBuildBreakdown_Overspend(t *testing.T) {
	in := breakdownInputs()
	in.Expenses = append(in.Expenses, commonExp(26, 3000000, "CHF", false))
	b := BuildBreakdown(in)
	if b.LeftOverCents >= 0 {
		t.Fatalf("fixture is not overspending: %d", b.LeftOverCents)
	}
	for _, f := range b.Sankey {
		if f.To == "k:left" {
			t.Errorf("negative surplus still drawn: %+v", f)
		}
	}
}

// A solo household draws no partner-B nodes; an empty one draws nothing.
func TestBuildBreakdown_Solo(t *testing.T) {
	in := breakdownInputs()
	in.PartnerB = model.User{}
	in.Incomes = in.Incomes[:1]
	in.Expenses = in.Expenses[:3] // drop Bo's personal row
	b := BuildBreakdown(in)
	if b.HasPartner {
		t.Error("HasPartner set for a solo household")
	}
	for _, f := range b.Sankey {
		if f.From == "in:b" {
			t.Errorf("partner B node in a solo household: %+v", f)
		}
	}
	for _, r := range b.Personal {
		if r.BCents != 0 {
			t.Errorf("partner B amount in a solo household: %+v", r)
		}
	}

	empty := Inputs{Household: in.Household, PartnerA: in.PartnerA, Display: "CHF", Rates: testRates}
	if e := BuildBreakdown(empty); !e.Empty || len(e.Sankey) != 0 {
		t.Errorf("empty household: Empty=%v flows=%d", e.Empty, len(e.Sankey))
	}
}
