package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/store"
	"github.com/mattmezza/paripari/internal/view"
)

var goalCategories = []string{"safety_net", "property", "education", "other"}

func goalCategoryLabel(c string) string {
	switch c {
	case "safety_net":
		return "Safety net"
	case "property":
		return "Property"
	case "education":
		return "Education"
	default:
		return "Other"
	}
}

// goalCard is a goal's computed progress plus the deadline/ETA framing the
// template renders directly (no date math in the template).
type goalCard struct {
	service.GoalProgress
	CategoryLabel string
	ETAText       string // "Mar 2027" or "" if unreachable
	StatusPill    string // "" | "positive" | "attention" | "danger"
	StatusText    string
	DeadlineDate  string // plain "YYYY-MM-DD", "" if none — .Goal.Deadline is a *string, templates can't dereference it
	PercentInt    int    // Ratio*100, rounded — for the progress bar's aria-valuenow
}

type goalsData struct {
	Goals      []goalCard
	Currencies []string
	CSRF       string
}

type goalFormData struct {
	Goal            model.Goal
	TargetAmountStr string // plain "1234.50", no currency prefix — pre-fills the edit input
	DeadlineDate    string // plain "YYYY-MM-DD", "" if none — .Goal.Deadline is a *string
	CSRF            string
}

// registerGoals mounts the goals section: named goals with progress bars
// (current balance drawn from savings/investment/pension accounts — see
// service.GoalProgresses) and inline htmx CRUD.
func registerGoals(mux *http.ServeMux, d *Deps) {
	mux.HandleFunc("GET /goals", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		gd, err := buildGoalsData(d, r)
		if err != nil {
			http.Error(w, "could not load goals", http.StatusInternalServerError)
			return
		}
		gd.CSRF = sess.Token
		d.View.Render(w, r, "goals", &view.PageData{
			Title: "Goals", Description: "Named savings goals and their projected completion date.", Data: gd,
		})
	})

	mux.HandleFunc("GET /goals/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		g, err := d.Store.Goal(sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		fd := &goalFormData{Goal: *g, TargetAmountStr: centsToPlain(g.TargetAmountCents), CSRF: sess.Token}
		if g.Deadline != nil {
			fd.DeadlineDate = *g.Deadline
		}
		d.View.Partial(w, "partials/goals-card-edit", fd)
	})

	mux.HandleFunc("GET /goals/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		card, err := buildGoalCard(d, r, sess.Household.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		d.View.Partial(w, "partials/goals-card", card)
	})

	mux.HandleFunc("POST /goals", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		g, formErr := parseGoalForm(r, sess.Household.ID)
		if formErr != "" {
			htmxFieldError(w, "#goal-form-error", formErr)
			return
		}
		if _, err := d.Store.CreateGoal(&g); err != nil {
			http.Error(w, "could not save goal", http.StatusInternalServerError)
			return
		}
		renderGoalsGrid(d, w, r, sess)
	})

	mux.HandleFunc("PUT /goals/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		g, formErr := parseGoalForm(r, sess.Household.ID)
		if formErr != "" {
			htmxFieldError(w, "#goal-form-error", formErr)
			return
		}
		g.ID = id
		if err := d.Store.UpdateGoal(&g); err != nil {
			http.Error(w, "could not save goal", http.StatusInternalServerError)
			return
		}
		renderGoalsGrid(d, w, r, sess)
	})

	mux.HandleFunc("DELETE /goals/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess := auth.FromContext(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err := d.Store.DeleteGoal(sess.Household.ID, id); err != nil {
			http.Error(w, "could not delete goal", http.StatusInternalServerError)
			return
		}
		renderGoalsGrid(d, w, r, sess)
	})
}

func renderGoalsGrid(d *Deps, w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	gd, err := buildGoalsData(d, r)
	if err != nil {
		http.Error(w, "could not load goals", http.StatusInternalServerError)
		return
	}
	gd.CSRF = sess.Token
	w.Header().Set("HX-Trigger", "pp:recalc")
	d.View.Partial(w, "partials/goals-grid", gd)
}

func buildGoalsData(d *Deps, r *http.Request) (*goalsData, error) {
	in, err := BuildInputs(d, r)
	if err != nil {
		return nil, err
	}
	ov := service.BuildOverview(in)
	progresses := service.GoalProgresses(in, ov)
	cards := make([]goalCard, 0, len(progresses))
	for _, p := range progresses {
		cards = append(cards, buildGoalCardFrom(p))
	}
	return &goalsData{Goals: cards, Currencies: view.Currencies}, nil
}

func buildGoalCard(d *Deps, r *http.Request, householdID, goalID int64) (*goalCard, error) {
	in, err := BuildInputs(d, r)
	if err != nil {
		return nil, err
	}
	ov := service.BuildOverview(in)
	for _, p := range service.GoalProgresses(in, ov) {
		if p.Goal.ID == goalID {
			c := buildGoalCardFrom(p)
			return &c, nil
		}
	}
	return nil, store.ErrNotFound
}

func buildGoalCardFrom(p service.GoalProgress) goalCard {
	c := goalCard{GoalProgress: p, CategoryLabel: goalCategoryLabel(p.Goal.Category), PercentInt: int(p.Ratio*100 + 0.5)}

	var projected time.Time
	switch {
	case p.ReachedAlready:
		c.StatusPill, c.StatusText = "positive", "Reached"
	case p.ETAMonths < 0:
		c.StatusPill, c.StatusText = "danger", "Unreachable at current pace"
	default:
		projected = time.Now().UTC().AddDate(0, p.ETAMonths, 0)
		c.ETAText = projected.Format("Jan 2006")
	}

	if p.Goal.Deadline != nil && *p.Goal.Deadline != "" {
		c.DeadlineDate = *p.Goal.Deadline
		if dl, err := time.Parse("2006-01-02", *p.Goal.Deadline); err == nil {
			switch {
			case p.ReachedAlready:
				// status already set above
			case p.ETAMonths < 0:
				// status already set above
			case !projected.After(dl):
				c.StatusPill, c.StatusText = "positive", "On track"+spareText(monthsBetween(projected, dl), "to spare")
			default:
				c.StatusPill, c.StatusText = "attention", "Behind schedule"+spareText(monthsBetween(dl, projected), "late")
			}
		}
	}
	return c
}

// monthsBetween counts whole calendar months from a to b (negative if b is
// earlier). ponytail: month granularity is all the ETA has — it is a whole
// number of months itself.
func monthsBetween(a, b time.Time) int {
	return (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
}

// spareText renders the margin on a deadline: " · 5 mo to spare", or nothing
// when the projection lands in the deadline's own month.
func spareText(months int, suffix string) string {
	if months <= 0 {
		return ""
	}
	return " · " + strconv.Itoa(months) + " mo " + suffix
}

func parseGoalForm(r *http.Request, householdID int64) (model.Goal, string) {
	r.ParseForm()
	g := model.Goal{HouseholdID: householdID}
	g.Name = r.PostFormValue("name")
	if g.Name == "" {
		return g, "Name is required."
	}
	g.Currency = r.PostFormValue("currency")
	if !ValidCurrency(g.Currency) {
		return g, "Choose a supported currency."
	}
	target, err := ParseAmount(r.PostFormValue("target_amount"))
	if err != nil || target <= 0 {
		return g, "Target amount must be greater than zero."
	}
	g.TargetAmountCents = target
	g.Category = r.PostFormValue("category")
	valid := false
	for _, c := range goalCategories {
		if c == g.Category {
			valid = true
			break
		}
	}
	if !valid {
		g.Category = "other"
	}
	if dl := r.PostFormValue("deadline"); dl != "" {
		if _, err := time.Parse("2006-01-02", dl); err != nil {
			return g, "Deadline is not a valid date."
		}
		g.Deadline = &dl
	}
	return g, ""
}
