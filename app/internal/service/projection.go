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
