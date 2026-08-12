package service

// Point is one month on a projection curve. MonthIndex 0 is today (the starting
// balance, before any contribution).
type Point struct {
	MonthIndex   int   `json:"m"`
	BalanceCents int64 `json:"v"`
}

// maxProjectionMonths caps open-ended searches (100 years).
const maxProjectionMonths = 1200

// ProjectSavings compounds monthly: each month the balance grows by
// annualReturnRate/12 and the contribution is added at month end.
func ProjectSavings(startCents, monthlyContributionCents int64, annualReturnRate float64, months int) []Point {
	if months < 0 {
		months = 0
	}
	r := annualReturnRate / 12
	pts := make([]Point, 0, months+1)
	balance := float64(startCents)
	pts = append(pts, Point{0, startCents})
	for m := 1; m <= months; m++ {
		balance = balance*(1+r) + float64(monthlyContributionCents)
		pts = append(pts, Point{m, int64(balance + 0.5*sign(balance))})
	}
	return pts
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// ProjectNetWorth returns the projected balance at each of the given year
// marks (e.g. 1, 5, 10, 20), keyed by year.
func ProjectNetWorth(startCents, monthlyContributionCents int64, annualReturnRate float64, years []int) map[int]int64 {
	max := 0
	for _, y := range years {
		if y > max {
			max = y
		}
	}
	pts := ProjectSavings(startCents, monthlyContributionCents, annualReturnRate, max*12)
	out := make(map[int]int64, len(years))
	for _, y := range years {
		if idx := y * 12; idx >= 0 && idx < len(pts) {
			out[y] = pts[idx].BalanceCents
		}
	}
	return out
}

// GoalETA returns the number of months until currentCents reaches targetCents
// with the given monthly contribution and annual return. 0 means "already
// there", -1 means unreachable within 100 years.
func GoalETA(currentCents, targetCents, monthlyContributionCents int64, annualReturnRate float64) int {
	if currentCents >= targetCents {
		return 0
	}
	r := annualReturnRate / 12
	balance := float64(currentCents)
	target := float64(targetCents)
	for m := 1; m <= maxProjectionMonths; m++ {
		next := balance*(1+r) + float64(monthlyContributionCents)
		if next <= balance && next < target {
			return -1 // not growing: never reachable
		}
		balance = next
		if balance >= target {
			return m
		}
	}
	return -1
}

// ProjectionSpec is ProjectSavings with the knobs the /projections screen
// exposes: a slice of the start held as cash, inflation, and dated one-offs.
type ProjectionSpec struct {
	StartCents   int64
	CashCents    int64 // part of StartCents kept uninvested — earns nothing
	GoldCents    int64 // part of StartCents held as gold — grows at GoldAnnualReturn
	MonthlyCents int64
	AnnualReturn float64
	// GoldAnnualReturn is gold's own nominal rate, discounted by the same
	// inflation as everything else.
	GoldAnnualReturn float64
	// AnnualInflation > 0 puts the whole curve in today's money: the invested
	// pot compounds at the real rate, cash shrinks, and the monthly
	// contribution is assumed to keep pace with inflation.
	AnnualInflation float64
	Events          map[int]int64 // month index → amount spent that month
	// Incomes is one entry per partner. Their pay is already counted inside
	// MonthlyCents; what these add is the *movement* on top of it.
	Incomes []IncomeStream
	Months  int
}

// Promotion is a timed step change to one partner's pay: from AtMonth on, their
// income is Pct higher (0.08 = +8%), on top of the ordinary growth.
type Promotion struct {
	AtMonth int
	Pct     float64
}

// IncomeStream is one partner's net monthly income and how it is assumed to
// move: a steady annual growth plus any promotions.
//
// AnnualGrowth is nominal, like every other rate here, so inflation discounts
// it: a 2% raise against 2% inflation leaves the contribution flat in today's
// money, which is what the projection was already assuming before this knob
// existed.
//
// ponytail: only the raise is modelled, not what it does to the rest of the
// household — expenses are assumed to keep pace with prices, so a raise lands
// on the surplus in full. Ceiling: no tax progression, no lifestyle creep.
type IncomeStream struct {
	MonthlyNetCents int64
	AnnualGrowth    float64
	Promotions      []Promotion
}

// Projection is a curve plus the figures the summary strip needs.
type Projection struct {
	Points           []Point
	GrowthCents      int64 // return credited over the horizon
	SpentCents       int64 // total of Events actually applied
	ContributedCents int64 // contributions paid in, raises included
}

// Project runs the three-bucket (invested + gold + cash) simulation. Spending
// comes out of cash first, then gold, then the invested pot — which can go
// negative, and is meant to: a plan that runs dry should look like it.
func Project(s ProjectionSpec) Projection {
	months := max(s.Months, 0)
	start := max(s.StartCents, 0)
	// Gold is a real holding, so it claims its share of the start first; the
	// user-typed cash slice gets clamped to whatever is left.
	gold := float64(min(max(s.GoldCents, 0), start))
	cash := min(float64(max(s.CashCents, 0)), float64(start)-gold)
	invested := float64(s.StartCents) - cash - gold

	i := s.AnnualInflation / 12
	// Real rates: nominal growth discounted by inflation. Cash earns nothing
	// nominally, so in today's money it decays at exactly the inflation rate.
	r := (1+s.AnnualReturn/12)/(1+i) - 1
	goldRate := (1+s.GoldAnnualReturn/12)/(1+i) - 1
	cashRate := 1/(1+i) - 1

	// One running multiplier per income stream, in today's money like the rest
	// of the curve.
	pay := make([]float64, len(s.Incomes))
	for i := range pay {
		pay[i] = 1
	}

	p := Projection{Points: make([]Point, 0, months+1)}
	p.Points = append(p.Points, Point{0, s.StartCents})
	var contributed float64
	for m := 1; m <= months; m++ {
		monthly := float64(s.MonthlyCents)
		for k, inc := range s.Incomes {
			pay[k] *= (1 + inc.AnnualGrowth/12) / (1 + i)
			for _, pr := range inc.Promotions {
				if pr.AtMonth == m {
					pay[k] *= 1 + pr.Pct
				}
			}
			monthly += float64(inc.MonthlyNetCents) * (pay[k] - 1)
		}
		contributed += monthly

		gain := invested*r + gold*goldRate + cash*cashRate
		p.GrowthCents += int64(gain + 0.5*sign(gain))
		invested += invested*r + monthly
		gold += gold * goldRate
		cash += cash * cashRate
		if out := s.Events[m]; out != 0 {
			p.SpentCents += out
			// Cash, then gold, then invested — invested is last because it is
			// the only bucket allowed to go negative, so it has to be the sink.
			take := float64(out)
			if d := min(cash, take); d > 0 {
				cash -= d
				take -= d
			}
			if d := min(gold, take); d > 0 {
				gold -= d
				take -= d
			}
			invested -= take
		}
		total := cash + gold + invested
		p.Points = append(p.Points, Point{m, int64(total + 0.5*sign(total))})
	}
	p.ContributedCents = int64(contributed + 0.5*sign(contributed))
	return p
}

// SeriesSpec describes one projection line.
type SeriesSpec struct {
	Name                     string
	StartCents               int64
	MonthlyContributionCents int64
	AnnualReturnRate         float64
	Months                   int
}

// Series is a named projection curve, ready for Chart.js.
type Series struct {
	Name   string  `json:"name"`
	Points []Point `json:"points"`
}

// MultiSeries generates one curve per spec, for side-by-side comparison.
func MultiSeries(specs []SeriesSpec) []Series {
	out := make([]Series, 0, len(specs))
	for _, s := range specs {
		out = append(out, Series{
			Name:   s.Name,
			Points: ProjectSavings(s.StartCents, s.MonthlyContributionCents, s.AnnualReturnRate, s.Months),
		})
	}
	return out
}
