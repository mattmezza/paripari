package service

import (
	"math"
	"testing"
)

func TestProjectSavings(t *testing.T) {
	tests := []struct {
		name           string
		start, monthly int64
		rate           float64
		months         int
		wantLen        int
		wantLast       int64
	}{
		{"zero rate is plain addition", 100000, 50000, 0, 12, 13, 100000 + 12*50000},
		{"no months returns just today", 100000, 50000, 0.05, 0, 1, 100000},
		{"negative months clamps", 100000, 50000, 0.05, -3, 1, 100000},
		{"no contribution, pure growth", 120000, 0, 0.12, 1, 2, 121200},
		{"zero everything", 0, 0, 0.05, 6, 7, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pts := ProjectSavings(tc.start, tc.monthly, tc.rate, tc.months)
			if len(pts) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(pts), tc.wantLen)
			}
			if pts[0].MonthIndex != 0 || pts[0].BalanceCents != tc.start {
				t.Errorf("point 0 = %+v, want start %d", pts[0], tc.start)
			}
			if got := pts[len(pts)-1].BalanceCents; got != tc.wantLast {
				t.Errorf("last = %d, want %d", got, tc.wantLast)
			}
		})
	}
}

// The monthly loop must agree with the closed-form annuity future value.
func TestProjectSavingsMatchesClosedForm(t *testing.T) {
	for _, rate := range []float64{0.01, 0.04, 0.07} {
		for _, months := range []int{12, 120, 240} {
			start, monthly := int64(5000000), int64(200000)
			pts := ProjectSavings(start, monthly, rate, months)
			r := rate / 12
			n := float64(months)
			want := float64(start)*math.Pow(1+r, n) +
				float64(monthly)*(math.Pow(1+r, n)-1)/r
			got := float64(pts[months].BalanceCents)
			if math.Abs(got-want)/want > 1e-6 {
				t.Errorf("rate %v months %d: got %v, closed form %v", rate, months, got, want)
			}
		}
	}
}

func TestProjectSavingsNegativeContribution(t *testing.T) {
	pts := ProjectSavings(100000, -20000, 0, 6)
	if got := pts[6].BalanceCents; got != -20000 {
		t.Errorf("drawdown last = %d, want -20000", got)
	}
}

func TestProjectNetWorth(t *testing.T) {
	got := ProjectNetWorth(1000000, 100000, 0.05, ProjectionYears)
	if len(got) != 4 {
		t.Fatalf("years = %d, want 4", len(got))
	}
	prev := int64(0)
	for _, y := range ProjectionYears {
		if got[y] <= prev {
			t.Errorf("year %d = %d, should exceed previous %d", y, got[y], prev)
		}
		prev = got[y]
	}
	full := ProjectSavings(1000000, 100000, 0.05, 240)
	if got[20] != full[240].BalanceCents {
		t.Errorf("20y = %d, want %d", got[20], full[240].BalanceCents)
	}
}

func TestGoalETA(t *testing.T) {
	tests := []struct {
		name                     string
		current, target, monthly int64
		rate                     float64
		want                     int
	}{
		{"already reached", 500000, 100000, 10000, 0, 0},
		{"exactly at target", 100000, 100000, 10000, 0, 0},
		{"ten months no growth", 0, 100000, 10000, 0, 10},
		{"growth arrives sooner", 0, 10000000, 100000, 0.10, 74},
		{"no contribution but growth gets there", 100000, 200000, 0, 0.10, 84},
		{"unreachable: no contribution, no growth", 100000, 200000, 0, 0, -1},
		{"unreachable: shrinking", 100000, 200000, -1000, 0, -1},
		{"unreachable within 100 years", 0, 1_000_000_000_000, 1, 0, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GoalETA(tc.current, tc.target, tc.monthly, tc.rate); got != tc.want {
				t.Errorf("GoalETA = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGoalETAAgreesWithProjection(t *testing.T) {
	current, target, monthly, rate := int64(200000), int64(5000000), int64(50000), 0.04
	m := GoalETA(current, target, monthly, rate)
	if m <= 0 {
		t.Fatalf("expected a reachable goal, got %d", m)
	}
	pts := ProjectSavings(current, monthly, rate, m)
	if pts[m].BalanceCents < target {
		t.Errorf("month %d balance %d below target %d", m, pts[m].BalanceCents, target)
	}
	if pts[m-1].BalanceCents >= target {
		t.Errorf("goal was already reached at month %d", m-1)
	}
}

func TestMultiSeries(t *testing.T) {
	got := MultiSeries([]SeriesSpec{
		{Name: "cash", StartCents: 100000, MonthlyContributionCents: 10000, AnnualReturnRate: 0, Months: 24},
		{Name: "invested", StartCents: 100000, MonthlyContributionCents: 10000, AnnualReturnRate: 0.07, Months: 24},
	})
	if len(got) != 2 {
		t.Fatalf("series = %d, want 2", len(got))
	}
	if got[0].Name != "cash" || len(got[0].Points) != 25 {
		t.Errorf("series 0 = %+v", got[0].Name)
	}
	if got[1].Points[24].BalanceCents <= got[0].Points[24].BalanceCents {
		t.Errorf("invested should outgrow cash")
	}
}
