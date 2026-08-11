package service

import "github.com/mattmezza/paripari/internal/model"

// Purity returns the fine-gold fraction for a karat value. The four common
// karats are pinned to the conventional multipliers; anything else is karat/24.
func Purity(karat int) float64 {
	switch karat {
	case 24:
		return 1.0
	case 22:
		return 0.9167
	case 18:
		return 0.75
	case 14:
		return 0.5833
	}
	if karat <= 0 {
		return 0
	}
	return float64(karat) / 24
}

// GoldValue values one gold item: weight × purity × quantity × 24K spot price
// per gram. pricePerGram24KCents must already be in the target currency.
func GoldValue(item model.GoldItem, pricePerGram24KCents int64) int64 {
	grams := item.WeightGrams * Purity(item.PurityKarat) * float64(item.Quantity)
	return scale(pricePerGram24KCents, grams)
}

// NetWorth is the consolidated asset picture, all in Currency.
type NetWorth struct {
	Currency string

	LiquidCents      int64 // accounts of every purpose
	AlternativeCents int64 // gold
	RealEstateCents  int64 // assets
	TotalCents       int64

	ByPurpose  map[string]int64 // checking | savings | investment | cc_buffer | envelope | pension
	FineGrams  float64          // total fine (24K-equivalent) gold weight
	GrossGrams float64
}

// LiquidOnlyCents is the "liquid only" view: cash and investments, no gold, no
// property.
func (n NetWorth) LiquidOnlyCents() int64 { return n.LiquidCents }

// ComputeNetWorth buckets accounts, gold and assets into the display currency.
func ComputeNetWorth(in Inputs) NetWorth {
	n := NetWorth{Currency: in.Display, ByPurpose: map[string]int64{}}
	for _, a := range in.Accounts {
		v := in.amount(a.BalanceCents, a.Currency)
		n.LiquidCents += v
		n.ByPurpose[a.Purpose] += v
	}
	for _, g := range in.Gold {
		n.AlternativeCents += GoldValue(g, in.GoldPricePerGramCents)
		n.GrossGrams += g.WeightGrams * float64(g.Quantity)
		n.FineGrams += g.WeightGrams * Purity(g.PurityKarat) * float64(g.Quantity)
	}
	for _, a := range in.Assets {
		n.RealEstateCents += in.amount(a.EstimatedValueCents, a.Currency)
	}
	n.TotalCents = n.LiquidCents + n.AlternativeCents + n.RealEstateCents
	return n
}

// GoalProgress is a goal with its computed progress and ETA.
type GoalProgress struct {
	Goal           model.Goal
	CurrentCents   int64 // in the goal's own currency
	TargetCents    int64
	Ratio          float64 // 0..1, capped at 1
	ETAMonths      int     // -1 = unreachable at the current saving rate
	MonthlyCents   int64   // contribution assumed, in the goal's currency
	ReachedAlready bool
}

// GoalProgresses evaluates every goal against the household's monthly surplus.
//
// ponytail: the schema links goals to no accounts, so progress is measured
// against savings + investment + pension balances, and every goal is assumed to
// be funded by the whole surplus. Add a goal_accounts link table if per-goal
// earmarking is ever needed.
func GoalProgresses(in Inputs, ov MonthlyOverview) []GoalProgress {
	nw := ComputeNetWorth(in)
	pot := nw.ByPurpose["savings"] + nw.ByPurpose["investment"] + nw.ByPurpose["pension"]
	out := make([]GoalProgress, 0, len(in.Goals))
	for _, g := range in.Goals {
		cur := conv(in.Rates, pot, in.Display, g.Currency)
		monthly := conv(in.Rates, ov.SurplusCents, ov.Currency, g.Currency)
		p := GoalProgress{
			Goal: g, CurrentCents: cur, TargetCents: g.TargetAmountCents,
			MonthlyCents: monthly,
			ETAMonths:    GoalETA(cur, g.TargetAmountCents, monthly, in.AnnualReturnRate),
		}
		if g.TargetAmountCents > 0 {
			p.Ratio = float64(cur) / float64(g.TargetAmountCents)
			if p.Ratio > 1 {
				p.Ratio = 1
			}
		}
		p.ReachedAlready = p.ETAMonths == 0
		out = append(out, p)
	}
	return out
}
