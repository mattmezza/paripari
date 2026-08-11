# PariPari — Build Conventions (agent contract)

Read `/home/matteo/dev/paripari/prompt.md` first. This file resolves everything the prompt leaves open. Do not deviate without a `ponytail:` comment explaining why.

## Locked decisions

- **Go**: latest stable, module `github.com/mattmezza/paripari`. Code lives under `app/`.
- **SQLite driver**: `modernc.org/sqlite` (pure Go, CGO_ENABLED=0 static binary).
- **Migrations**: tiny in-house runner in `internal/store/migrate.go` (embed `app/migrations/*.sql`, apply in filename order, track in `schema_migrations` table). No golang-migrate dependency.
- **Auth**: email + password (bcrypt via `golang.org/x/crypto/bcrypt`), secure HTTP-only cookie sessions stored in DB (`sessions` table). CSRF: same-site strict cookies + custom header check for htmx POSTs (htmx sends `HX-Request`; also embed a csrf token input in forms). No passkeys in v1.
- **Onboarding**: first user signs up → creates household → gets invite code (random token, shown in settings/onboarding). Partner signs up with the code and joins. Signup for a household closes at 2 members.
- **History**: `net_worth_snapshots` table (household_id, date, per-bucket totals in a JSON column or normalized columns liquid/alternative/real_estate, valued in a base currency = CHF + also store display-currency-agnostic: store per-currency? NO — store converted totals in CHF at snapshot time). Snapshot appended on any balance/asset/gold edit (max 1 per day per household, upsert on date) + daily background ticker in main.go.
- **FX**: frankfurter.app, cached in `fx_rates`, refresh ≤1/day + manual refresh button. Base pairs vs CHF, EUR, TRY, USD, GBP.
- **Gold**: gold-api.com (`https://api.gold-api.com/price/XAU`, free, no key) → USD/oz → per gram (÷31.1034768) → FX-convert. Cache in `gold_prices`. Manual price override in settings as fallback.
- **Default display currency**: CHF, changeable per household in settings.
- **Money**: store amounts as INTEGER cents (minor units). Format helpers with thousands separators (Swiss style `1'234.50` for CHF, standard for others — use golang.org/x/text/... NO: hand-roll one small formatter, apostrophe for CHF, narrow-space/comma otherwise. Keep it in `internal/service/money.go`).
- **Purity multipliers**: 24K=1.0, 22K=0.9167, 18K=0.75, 14K=0.5833.

## Frontend

- **Tailwind CSS v4 via standalone CLI** (`tailwindcss` binary downloaded by `make tools` into `./bin/`, gitignored). Source `app/static/css/app.css` (with `@import "tailwindcss"` + `@theme` tokens), built to `app/static/css/app.min.css` (committed, so Docker build needs no Node). All design tokens are OKLCH CSS custom properties declared in the `@theme` block — components reference tokens, never raw hex.
- **htmx + Alpine.js + Chart.js**: vendored into `app/static/js/` (downloaded once, committed) — NOT CDN, because PWA offline. Pin versions in the Makefile `vendor` target.
- **Fonts**: self-hosted woff2 in `app/static/fonts/` — Space Grotesk (display), Inter (body, `font-feature-settings: "tnum"` for figures). No Google Fonts CDN.
- **Templates**: Go `html/template`. `app/templates/layouts/base.html` (full HTML shell, SEO/OG meta per prompt) + per-section files `app/templates/<section>.html` + partials `app/templates/partials/*.html`.
- **Render contract** (owned by foundation, in `internal/view`):
  - `view.Render(w, r, "expenses", data)` — renders full page inside base layout, or bare content when `HX-Request: true` header present (htmx navigation uses `hx-boost` so full-page swaps are fine; explicit partials rendered with `view.Partial(w, "partials/transfer-table", data)`).
  - `PageData` fields available in every template: `Title`, `Description`, `Path`, `BaseURL`, `CanonicalURL`, `User`, `Partner`, `Household`, `DisplayCurrency`, `Currencies`, `CSRF`, `Flash`, `TransfersChanged` (bool flag), `Data` (any, page-specific).
  - Template funcs: `money cents currency`, `moneyIn cents fromCur` (converts to display currency), `pct`, `date`, plus `dict`, `json`.
- **Number ticking / recalc pulse**: Alpine helper + CSS in the design system; htmx swaps trigger `pp:recalc` events.

## File ownership (to avoid agent collisions)

- Foundation agent: everything under `app/` Go code except `internal/service/{engine,projection,scenario}*`, plus `Makefile`, `Dockerfile`, `docker-compose.yml`, `.github/`, `.gitignore`. Creates stub handlers per section in separate files (`internal/handler/dashboard.go`, `expenses.go`, …) each with its own `registerX(mux, deps)` function.
- Engine agent: `internal/service/` calculation code + `_test.go` files only.
- FX/Gold agent: `internal/fx/`, `internal/gold/`, snapshot job.
- Design agent: `app/templates/layouts/`, `app/templates/auth*.html`, `app/static/**` (css, js vendor, fonts, icons, manifest, sw.js, og-image, favicons), robots.txt/sitemap handlers content.
- Screen agents: only their own `internal/handler/<section>.go` + `app/templates/<section>*.html` + `app/templates/partials/<section>-*.html`.

## Design system (Hallmark — locked)

- Genre: modern-minimal. Calm precise instrument. Mobile-first at 380px.
- Paper: light `oklch(98.5% 0.003 240)`, dark `oklch(17% 0.01 240)`. Dark mode via `prefers-color-scheme` AND a manual toggle (class `dark` on `<html>`, persisted in localStorage).
- Accent (single): cobalt `oklch(52% 0.19 262)`. Positive/growth: `oklch(60% 0.13 160)`. Warning/attention: `oklch(68% 0.15 60)`. Danger: `oklch(55% 0.19 25)`. Neutrals from the paper hue. Nothing else chromatic.
- Type: Space Grotesk 500/700 for display/headings, Inter 400/500/600 body, tabular numerals for ALL figures. No italic headings.
- Numbers are the UI: big, tabular, right-aligned in tables, currency symbols muted.
- The "pari" idea: partner A/B always rendered as balanced pairs — two equal columns, mirrored layouts, same visual weight. Partner accent tints: A = accent, B = `oklch(52% 0.12 200)` (teal-leaning cobalt sibling) — used ONLY in charts/split bars, sparingly.
- Motion: 150–250ms, `transform`/`opacity` only, three named easings, number tick animation on recalc, `prefers-reduced-motion` collapses to crossfade. Focus rings instant, ≥3:1.
- Touch targets ≥44px, no hover-only interactions, 8 interaction states on all controls.
- Charts (Chart.js): tokens-driven colors, no gridline clutter, smooth monotone lines, scenario overlays = solid current vs dashed scenario, goal markers as subtle vertical annotations.

## Testing / CI

- Engine: table-driven unit tests (this is the money math — thorough).
- Handlers: a few httptest smoke tests (login → dashboard 200).
- `make test` = `go test ./...` from `app/`. CI (`ci.yml`): on PR + push to main, runs-on self-hosted, go test + go vet + build. `release.yml`: on release published, docker build + push `ghcr.io/mattmezza/paripari` (tags: version + latest), runs-on self-hosted.
