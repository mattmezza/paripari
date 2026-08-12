package service

import (
	"sort"

	"github.com/mattmezza/paripari/internal/model"
)

// Uncategorised is the bucket name for expenses with no subcategory. Grouping
// them under one label beats a nameless slice in every chart.
const Uncategorised = "Uncategorised"

// BreakdownRow is one subcategory total inside the common pool.
type BreakdownRow struct {
	Subcategory string `json:"label"`
	AmountCents int64  `json:"value"`
	// Savings is true when every expense in the row is savings-tagged: the
	// money is kept, not spent, and the charts tint it accordingly.
	Savings bool `json:"savings"`
	// ShareBP is the row as basis points of household net income (pct100).
	ShareBP int64 `json:"-"`
	Count   int   `json:"-"`
	// SpentCents is the part of the row that is actually spent — the sankey's
	// expense branch carries this, not AmountCents, because savings-tagged
	// money leaves through the "not spent" branch instead.
	SpentCents int64 `json:"-"`
}

// PersonalRow is one subcategory across both partners, so the personal chart
// can put A and B on the same row.
type PersonalRow struct {
	Subcategory string `json:"label"`
	ACents      int64  `json:"a"`
	BCents      int64  `json:"b"`
	Savings     bool   `json:"savings"`
	ShareBP     int64  `json:"-"`
	SpentCents  int64  `json:"-"`
	// ASpentCents and BSpentCents split SpentCents between the partners.
	ASpentCents, BSpentCents int64 `json:"-"`
}

// KeptRow is one destination for money that is not spent: an account purpose
// fed by savings-tagged expenses, or whatever is simply left over.
type KeptRow struct {
	Purpose     string // savings | investment | pension | envelope | ... | left
	Label       string
	AmountCents int64
	// ACents and BCents attribute the row to the partners: a personal row to
	// its owner, a common one by the split ratio. Together they make up
	// AmountCents.
	ACents, BCents int64
	// ACommonCents and BCommonCents are the parts of A's and B's shares that
	// come out of the common pool; the rest is their own money. Kept apart
	// from the totals because the sankey routes them through different nodes,
	// and re-deriving them by re-splitting a rounded total would lose cents.
	ACommonCents, BCommonCents int64
}

// CommonCents is the part of the row funded from the common pool.
func (r KeptRow) CommonCents() int64 { return r.ACommonCents + r.BCommonCents }

// AOwnCents and BOwnCents are the parts a partner pays out of their own money.
func (r KeptRow) AOwnCents() int64 { return r.ACents - r.ACommonCents }
func (r KeptRow) BOwnCents() int64 { return r.BCents - r.BCommonCents }

// Flow is one sankey link. Kind tags the link for colouring: income, common,
// personal, savings, surplus.
type Flow struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Cents int64  `json:"flow"`
	Kind  string `json:"kind"`
}

// Breakdown is everything the expenses-analysis screen renders: subcategory
// totals for the tables and doughnuts, plus the income-to-expense sankey.
type Breakdown struct {
	Currency string `json:"currency"`
	// Common and Personal are sorted by amount, largest first.
	Common   []BreakdownRow `json:"common"`
	Personal []PersonalRow  `json:"personal"`
	Sankey   []Flow         `json:"sankey"`
	// Columns pins each sankey node to a layer, so income always sits left and
	// subcategories always sit right even when a bucket is empty. Labels maps
	// the node keys to display names: keys are prefixed (a common "groceries"
	// and a personal one are different money) and must never collide with a
	// partner's name.
	Columns map[string]int    `json:"columns"`
	Labels  map[string]string `json:"labels"`
	// Priority is the vertical order within a column. Left to itself the chart
	// sorts nodes by size, which drags the kept branch above the spending one
	// and makes every ribbon cross; this keeps the two branches apart and the
	// partners in the same order everywhere.
	Priority map[string]int `json:"priority"`

	PartnerAName string `json:"aName"`
	PartnerBName string `json:"bName"`
	HasPartner   bool   `json:"hasPartner"`

	NetIncomeCents int64 `json:"-"`
	CommonCents    int64 `json:"-"`
	APersonalCents int64 `json:"-"`
	BPersonalCents int64 `json:"-"`
	LeftOverCents  int64 `json:"-"` // household available; negative means overspend
	SavingsCents   int64 `json:"-"`
	SpentCents     int64 `json:"-"` // everything not tagged as savings
	// Kept lists where the non-spent money goes, largest first.
	Kept []KeptRow `json:"-"`
	// Empty is true when there is nothing to chart.
	Empty bool `json:"-"`
}

// bucket accumulates one subcategory while grouping.
type bucket struct {
	a, b, total, spent int64
	aSpent, bSpent     int64
	allSavings         bool
	count              int
}

// keptBucket accumulates one not-spent destination: each partner's own money
// and each partner's share of the common pool, kept apart.
type keptBucket struct{ aOwn, bOwn, aCommon, bCommon int64 }

// BuildBreakdown groups the household's monthly expenses by subcategory and
// lays out the income sankey. Pure function of Inputs, like every other engine
// entry point; all amounts land in the display currency.
func BuildBreakdown(in Inputs) Breakdown {
	ov := BuildOverview(in)

	b := Breakdown{
		Currency:       in.Display,
		Columns:        map[string]int{},
		PartnerAName:   ov.A.Name,
		PartnerBName:   ov.B.Name,
		HasPartner:     in.PartnerB.ID != 0,
		NetIncomeCents: ov.TotalIncomeCents,
		SpentCents:     ov.CommonExpensesCents + ov.PersonalExpensesCents,
		CommonCents:    ov.CommonExpensesCents + ov.CommonSavingsCents,
		APersonalCents: ov.A.PersonalExpensesCents + ov.A.PersonalSavingsCents,
		BPersonalCents: ov.B.PersonalExpensesCents + ov.B.PersonalSavingsCents,
		LeftOverCents:  ov.AvailableCents,
		SavingsCents:   ov.TotalSavingsCents,
	}

	common := map[string]*bucket{}
	personal := map[string]*bucket{}
	var commonOrder, personalOrder []string
	add := func(m map[string]*bucket, order *[]string, key string, cents int64, savings, isB bool) {
		e, ok := m[key]
		if !ok {
			e = &bucket{allSavings: true}
			m[key] = e
			*order = append(*order, key)
		}
		e.total += cents
		e.count++
		if !savings {
			e.spent += cents
		}
		if isB {
			e.b += cents
			if !savings {
				e.bSpent += cents
			}
		} else {
			e.a += cents
			if !savings {
				e.aSpent += cents
			}
		}
		e.allSavings = e.allSavings && savings
	}

	// Money that is kept is grouped by its tag: an investment or a pension
	// contribution is not plain savings. A row tagged only "savings" falls
	// back to the purpose of the account it lands in, so a household that
	// expresses the same thing through accounts still gets the split; naming
	// no account at all means the household default, a savings account.
	purposeOf := map[int64]string{}
	for _, a := range in.Accounts {
		purposeOf[a.ID] = a.Purpose
	}
	// Kept money is tracked per purpose, per partner, and by whether it came
	// out of the common pool or a partner's own money — the sankey routes
	// those through different nodes, and its last column answers "how much of
	// this is mine?", which a total cannot.
	kept := map[string]*keptBucket{}
	var keptOrder []string
	addKept := func(purpose string, own, viaCommon int64, isB bool) {
		e, ok := kept[purpose]
		if !ok {
			e = &keptBucket{}
			kept[purpose] = e
			keptOrder = append(keptOrder, purpose)
		}
		if isB {
			e.bOwn, e.bCommon = e.bOwn+own, e.bCommon+viaCommon
		} else {
			e.aOwn, e.aCommon = e.aOwn+own, e.aCommon+viaCommon
		}
	}

	for _, e := range in.Expenses {
		amt := in.amount(e.AmountCents, e.Currency)
		if e.IsSavings() {
			purpose := e.Kind
			if purpose == model.KindSavings && e.AccountID != nil {
				if p, ok := purposeOf[*e.AccountID]; ok && p != "" {
					purpose = p
				}
			}
			if e.Category == "personal" {
				isB := e.UserID != nil && ov.B.UserID != 0 && *e.UserID == ov.B.UserID
				addKept(purpose, amt, 0, isB)
			} else {
				// A common row belongs to both partners, in the same
				// proportion they fund every other common expense.
				ca, cb := ShareOf(amt, ov.Ratio)
				addKept(purpose, 0, ca, false)
				addKept(purpose, 0, cb, true)
			}
		}
		sub := e.Subcategory
		if sub == "" {
			sub = Uncategorised
		}
		if e.Category == "personal" {
			// An orphan personal expense (owner not in this household) is
			// dropped, exactly as BuildOverview drops it.
			if e.UserID == nil {
				continue
			}
			switch *e.UserID {
			case ov.A.UserID:
				add(personal, &personalOrder, sub, amt, e.IsSavings(), false)
			case ov.B.UserID:
				if ov.B.UserID == 0 {
					continue
				}
				add(personal, &personalOrder, sub, amt, e.IsSavings(), true)
			}
			continue
		}
		add(common, &commonOrder, sub, amt, e.IsSavings(), false)
	}

	for _, k := range commonOrder {
		e := common[k]
		b.Common = append(b.Common, BreakdownRow{
			Subcategory: k, AmountCents: e.total, Savings: e.allSavings,
			ShareBP: rateBP(e.total, ov.TotalIncomeCents), Count: e.count,
			SpentCents: e.spent,
		})
	}
	for _, k := range personalOrder {
		e := personal[k]
		b.Personal = append(b.Personal, PersonalRow{
			Subcategory: k, ACents: e.a, BCents: e.b, Savings: e.allSavings,
			ShareBP: rateBP(e.total, ov.TotalIncomeCents), SpentCents: e.spent,
			ASpentCents: e.aSpent, BSpentCents: e.bSpent,
		})
	}
	// Largest first; ties keep insertion order so the list is stable between
	// renders.
	sort.SliceStable(b.Common, func(i, j int) bool { return b.Common[i].AmountCents > b.Common[j].AmountCents })
	sort.SliceStable(b.Personal, func(i, j int) bool {
		return b.Personal[i].ACents+b.Personal[i].BCents > b.Personal[j].ACents+b.Personal[j].BCents
	})

	b.Kept = keptRows(kept, keptOrder, ov)
	b.Sankey, b.Columns, b.Labels, b.Priority = buildSankey(ov, b)
	b.Empty = len(b.Common) == 0 && len(b.Personal) == 0 && ov.TotalIncomeCents == 0
	return b
}

// keptPurposeLabel names an account purpose for display. Shared with the
// accounts screen so one purpose reads the same everywhere.
var keptPurposeLabel = map[string]string{
	"checking":   "Checking",
	"savings":    "Savings",
	"investment": "Investment",
	"envelope":   "Budget envelopes",
	"pension":    "Pension",
	"cc_buffer":  "Credit card buffer",
}

// PurposeLabel is the display name for an account purpose. Unknown purposes
// are returned unchanged rather than hidden.
func PurposeLabel(purpose string) string {
	if l, ok := keptPurposeLabel[purpose]; ok {
		return l
	}
	return purpose
}

// keptRows orders the not-spent destinations: the savings purposes largest
// first, then whatever is simply left over. A negative available figure means
// the household overspends and there is nothing left to show.
func keptRows(kept map[string]*keptBucket, order []string, ov MonthlyOverview) []KeptRow {
	var rows []KeptRow
	for _, purpose := range order {
		e := kept[purpose]
		a, b := e.aOwn+e.aCommon, e.bOwn+e.bCommon
		if a+b > 0 {
			rows = append(rows, KeptRow{
				Purpose: purpose, Label: PurposeLabel(purpose), AmountCents: a + b,
				ACents: a, BCents: b, ACommonCents: e.aCommon, BCommonCents: e.bCommon,
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].AmountCents > rows[j].AmountCents })
	if ov.AvailableCents > 0 {
		// Left over is a partner's own money by construction: it is what they
		// still hold after paying everything, common share included.
		rows = append(rows, KeptRow{
			Purpose: "left", Label: "Left over", AmountCents: ov.AvailableCents,
			ACents: max0(ov.A.AvailableCents), BCents: max0(ov.B.AvailableCents),
		})
	}
	return rows
}

// max0 clamps a partner's available figure: one partner overspending must not
// eat into the other's ribbon.
func max0(cents int64) int64 {
	if cents < 0 {
		return 0
	}
	return cents
}

// buildSankey lays the flows out in three columns:
//
//	partner income → spent and not spent, each split into the common pool and
//	each partner's own money → subcategories for what is spent, destinations
//	for what is kept
//
// There is no "common versus personal" node in the middle: the split already
// happened one column earlier, and a node that only passes its inflow through
// makes every ribbon cross twice for no information.
//
// Savings-tagged money leaves through the not-spent branch, so the last
// column carries spending by subcategory beside every destination the kept
// money reaches. Who owns which part of it is already answered one column
// earlier, and in the table below the chart. Zero and negative flows are
// dropped: a sankey cannot draw them, and a household spending more than it
// earns has no "left over" ribbon.
func buildSankey(ov MonthlyOverview, b Breakdown) ([]Flow, map[string]int, map[string]string, map[string]int) {
	cols, labels, prio := map[string]int{}, map[string]string{}, map[string]int{}
	var flows []Flow
	// Nodes are declared top to bottom within a column; the running counter is
	// what the chart uses to order them.
	order := 0
	node := func(key, label string, col int) string {
		if _, seen := cols[key]; !seen {
			prio[key] = order
			order++
		}
		cols[key], labels[key] = col, label
		return key
	}
	link := func(from, to string, cents int64, kind string) bool {
		if cents <= 0 {
			return false
		}
		flows = append(flows, Flow{From: from, To: to, Cents: cents, Kind: kind})
		return true
	}

	const spendCommon, keepCommon = "sp:common", "kp:common"
	side := []string{"a", "b"}
	partners := []PartnerCashflow{ov.A, ov.B}
	present := func(i int) bool { return partners[i].UserID != 0 }

	// Declared in reading order — spending branch first, then the kept one, so
	// the two never interleave vertically.
	for i, p := range partners {
		if present(i) {
			node("in:"+side[i], p.Name, 0)
		}
	}
	if ov.CommonExpensesCents > 0 {
		node(spendCommon, "Spent · common", 1)
	}
	for i, p := range partners {
		if present(i) && p.PersonalExpensesCents > 0 {
			node("sp:"+side[i], "Spent · "+p.Name, 1)
		}
	}
	if ov.CommonSavingsCents > 0 {
		node(keepCommon, "Kept · common", 1)
	}
	for i, p := range partners {
		if present(i) && p.PersonalSavingsCents+max0(p.AvailableCents) > 0 {
			node("kp:"+side[i], "Kept · "+p.Name, 1)
		}
	}
	for _, r := range b.Common {
		if r.SpentCents > 0 {
			node("c:"+r.Subcategory, r.Subcategory, 2)
		}
	}
	for _, r := range b.Personal {
		if r.SpentCents > 0 {
			node("p:"+r.Subcategory, r.Subcategory, 2)
		}
	}
	for _, r := range b.Kept {
		node("k:"+r.Purpose, r.Label, 2)
	}
	// Column 0 → 1: each partner's income, split four ways — what they spend
	// on the common pool and on themselves, and the same for what they keep.
	for i, p := range partners {
		if !present(i) {
			continue
		}
		src := "in:" + side[i]
		link(src, spendCommon, p.CommonShareCents-p.CommonSavingsShareCents, "expense")
		link(src, "sp:"+side[i], p.PersonalExpensesCents, "expense")
		link(src, keepCommon, p.CommonSavingsShareCents, "kept")
		link(src, "kp:"+side[i], p.PersonalSavingsCents+max0(p.AvailableCents), "kept")
	}

	// Column 1 → 2: straight out into the subcategories. A personal
	// subcategory both partners use is one node fed by both.
	for _, r := range b.Common {
		link(spendCommon, "c:"+r.Subcategory, r.SpentCents, "common")
	}
	for _, r := range b.Personal {
		link("sp:a", "p:"+r.Subcategory, r.ASpentCents, "personal")
		link("sp:b", "p:"+r.Subcategory, r.BSpentCents, "personal")
	}

	// Column 1 → 2 → 3, kept side: into each destination, then back out per
	// partner. Left over never passes through the common pool — it is what
	// each partner still holds.
	for _, r := range b.Kept {
		kind := "savings"
		if r.Purpose == "left" {
			kind = "surplus"
		}
		dest := "k:" + r.Purpose
		link(keepCommon, dest, r.CommonCents(), kind)
		link("kp:a", dest, r.AOwnCents(), kind)
		link("kp:b", dest, r.BOwnCents(), kind)
	}

	// A node declared for a branch that turned out empty would be drawn as a
	// stub with no ribbon.
	used := map[string]bool{}
	for _, f := range flows {
		used[f.From], used[f.To] = true, true
	}
	for key := range cols {
		if !used[key] {
			delete(cols, key)
			delete(labels, key)
			delete(prio, key)
		}
	}
	return flows, cols, labels, prio
}
