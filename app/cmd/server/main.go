// Command server is the PariPari HTTP server.
package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/fx"
	"github.com/mattmezza/paripari/internal/gold"
	"github.com/mattmezza/paripari/internal/handler"
	"github.com/mattmezza/paripari/internal/jobs"
	"github.com/mattmezza/paripari/internal/store"
	"github.com/mattmezza/paripari/internal/view"
	"github.com/mattmezza/paripari/static"
	"github.com/mattmezza/paripari/templates"
)

type config struct {
	Port          string
	DataDir       string
	BaseURL       string
	SecureCookies bool
	Dev           bool
}

func loadConfig() config {
	c := config{
		Port:          env("PORT", "8080"),
		DataDir:       env("DATA_DIR", "./data"),
		BaseURL:       strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/"),
		SecureCookies: env("SECURE_COOKIES", "") == "1",
		Dev:           env("DEV", "") == "1",
	}
	return c
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	seedFlag := flag.Bool("seed", false, "insert demo data into an empty database and exit")
	flag.Parse()

	cfg := loadConfig()
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "paripari.db"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if *seedFlag {
		if err := seed(st); err != nil {
			log.Fatalf("seed: %v", err)
		}
		return
	}

	// Templates and static assets are embedded; DEV=1 reads them from disk so
	// edits show up without a rebuild.
	tmplFS, staticFS := fs.FS(templates.FS), fs.FS(static.FS)
	if cfg.Dev {
		tmplFS, staticFS = os.DirFS("templates"), os.DirFS("static")
	}
	v, err := view.New(tmplFS, cfg.BaseURL, cfg.Dev)
	if err != nil {
		log.Fatalf("templates: %v", err)
	}
	rates := fx.NewProvider(st)
	goldPrices := gold.NewProvider(st, rates)
	jobs.Default = jobs.NewSnapshotter(st, rates, goldPrices)

	v.Rates = rates
	v.Context = func(r *http.Request) view.ContextData {
		s := auth.FromContext(r)
		if s == nil {
			return view.ContextData{}
		}
		return view.ContextData{User: s.User, Partner: s.Partner, Household: s.Household, CSRF: s.Token}
	}

	deps := &handler.Deps{
		Store:   st,
		View:    v,
		Auth:    auth.NewManager(st, cfg.SecureCookies),
		Rates:   rates,
		Gold:    goldPrices,
		BaseURL: cfg.BaseURL,
		Static:  staticFS,
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           logRequests(handler.New(deps)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go runDaily(ctx, st)

	go func() {
		log.Printf("paripari listening on %s (dev=%v)", srv.Addr, cfg.Dev)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// DailyJob is a background task run at startup and then once a day. The fx,
// gold and snapshot implementations plug in via dailyJobs below.
type DailyJob interface {
	Name() string
	Run(context.Context) error
}

// dailyJobs returns the background jobs to run at startup and once a day.
func dailyJobs(st *store.Store) []DailyJob {
	return []DailyJob{
		sessionGC{st},
		fx.NewRefresher(st),
		gold.NewRefresher(st),
		jobs.Default,
	}
}

func runDaily(ctx context.Context, st *store.Store) {
	jobs := dailyJobs(st)
	run := func() {
		for _, j := range jobs {
			if err := j.Run(ctx); err != nil {
				log.Printf("daily job %s: %v", j.Name(), err)
			}
		}
	}
	run()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

type sessionGC struct{ st *store.Store }

func (sessionGC) Name() string                { return "session-gc" }
func (s sessionGC) Run(context.Context) error { return s.st.DeleteExpiredSessions() }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
