package handler

import (
	"net/http"

	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/view"
)

// dashboardData is what the dashboard template renders.
type dashboardData struct {
	Empty            bool // no incomes AND no expenses: show the setup checklist
	HasIncomes       bool
	HasExpenses      bool
	HasPartner       bool
	Overview         service.MonthlyOverview
	NetWorth         service.NetWorth
	Goals            []goalCard
	TransfersChanged bool
}

// registerDashboard mounts the dashboard section: the monthly picture per
// prompt.md Key Screen 1.
func registerDashboard(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		in, err := BuildInputs(d, r)
		if err != nil {
			http.Error(w, "could not load household", http.StatusInternalServerError)
			return
		}

		ov := service.BuildOverview(in)
		data := &dashboardData{
			HasIncomes:  len(in.Incomes) > 0,
			HasExpenses: len(in.Expenses) > 0,
			HasPartner:  in.PartnerB.ID != 0,
			Overview:    ov,
			NetWorth:    service.ComputeNetWorth(in),
		}
		// Same cards as /goals: percent, on-track state and ETA come precomputed.
		for _, p := range service.GoalProgresses(in, ov) {
			data.Goals = append(data.Goals, buildGoalCardFrom(p))
		}
		data.Empty = !data.HasIncomes && !data.HasExpenses

		if last, err := d.Store.LastTransferConfirmation(in.Household.ID); err == nil {
			plan := service.BuildTransfers(in, ov)
			plan.Diff(last.Payload)
			data.TransfersChanged = plan.Changed
		}

		d.View.Render(w, r, "dashboard", &view.PageData{Data: data})
	})
}
