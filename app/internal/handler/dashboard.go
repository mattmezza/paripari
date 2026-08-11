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
	Goals            []service.GoalProgress
	TransfersChanged bool
	// GrossAsNetA/B: this partner has an income source with no deductions
	// recorded, so their "net income" is really gross.
	GrossAsNetA, GrossAsNetB bool
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
			Goals:       service.GoalProgresses(in, ov),
		}
		data.Empty = !data.HasIncomes && !data.HasExpenses
		for _, src := range in.Incomes {
			if len(src.Deductions) > 0 {
				continue
			}
			switch src.UserID {
			case in.PartnerA.ID:
				data.GrossAsNetA = true
			case in.PartnerB.ID:
				data.GrossAsNetB = true
			}
		}

		if last, err := d.Store.LastTransferConfirmation(in.Household.ID); err == nil {
			plan := service.BuildTransfers(in, ov)
			plan.Diff(last.Payload)
			data.TransfersChanged = plan.Changed
		}

		d.View.Render(w, r, "dashboard", &view.PageData{Data: data})
	})
}
