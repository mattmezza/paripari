# PariPari Architecture

PariPari is a single-container, server-driven app. All logic runs on the server; the browser is a view layer. This document outlines the request flow, codebase structure, database schema, and calculation engines.

## Request flow

1. **Browser** sends an HTTP request (GET or POST)
2. **Handler** in `internal/handler` receives the request, validates auth, extracts session data
3. **Service** layer in `internal/service` performs calculations (split ratios, projections, scenarios)
4. **Store** layer in `internal/store` queries/updates the SQLite database
5. **View** layer in `internal/view` renders a Go `html/template`
   - If the request has the `HX-Request: true` header (htmx partial update), renders just the changed fragment
   - Otherwise, renders the full page inside `layouts/base.html`
6. **Response** returns HTML (or JSON for API endpoints)

### htmx flow

For inline edits (e.g. changing an expense amount):

1. User types a new value in the browser
2. htmx sends a POST to the handler with the new value
3. Handler calls the service to recalculate affected values
4. Handler returns an HTML fragment (e.g. the updated transfer table)
5. htmx replaces the DOM element with the new HTML (no full page reload)

## Package structure

### app/cmd/server/main.go

Entry point. Loads config from environment variables, initializes the store, FX provider, gold price provider, and HTTP server. Runs background jobs (daily: session cleanup, FX/gold refresh, net worth snapshots).

### app/internal/handler

HTTP handlers for each section:

- `dashboard.go` — GET /dashboard
- `auth.go` — GET/POST /login, /signup, /logout
- `income.go` — GET/POST /income
- `expenses.go` — GET/POST /expenses, GET /expenses/analysis (subcategory breakdown + income sankey)
- `accounts.go` — GET/POST /accounts
- `transfers.go` — GET /transfers
- `net_worth.go` — GET /net-worth
- `gold.go` — GET/POST /gold
- `goals.go` — GET/POST /goals
- `scenarios.go` — GET/POST /scenarios
- `trips.go` — GET/POST /trips
- `projections.go` — GET /projections, GET /projections/data (chart JSON), POST/PUT/DELETE /projections/saved (named sets of assumptions)
- `settings.go` — GET/POST /settings

Each handler is registered via a `registerX(mux *http.ServeMux, deps *Deps)` function. Handlers accept `*Deps` (store, view, auth, rates, gold provider).

### app/internal/view

Template rendering. `view.New()` loads templates from embedded FS (or disk in DEV mode). `view.Render()` renders a full page (with base layout) or a bare fragment (if htmx request). Template functions: `money`, `moneyIn`, `pct`, `date`, `dict`, `json`.

### app/internal/store

SQLite repository layer. `Open()` initializes the database, `Migrate()` applies schema migrations. Methods:

- `GetHousehold(id)` — fetch household
- `GetUser(id)`, `ListUsers(household_id)` — user queries
- `ListExpenses(household_id)` — fetch all expenses
- `UpdateExpense(id, name, amount, ...)` — update and return updated row
- Similar methods for income, accounts, goals, scenarios, etc.

Queries are parameterized (prepared statements). All money amounts are stored as INTEGER (cents). Timestamps are TEXT (ISO8601 UTC).

### app/internal/service

Business logic:

- **Split calculator** (`split.go`): Computes each partner's contribution ratio based on split method (50/50 or income-weighted) and income data.
- **Projection engine** (`projection.go`): Simulates monthly cash flow forward N years, with compound growth assumptions (0% for cash, 4-7% for investments, etc.).
- **Scenario engine** (`scenario.go`): Takes a base household state, applies a named scenario's parameter changes, recalculates all downstream metrics (split, cash flow, projections).
- **Transfer calculator** (`transfer.go`): Produces the monthly transfer table (who owes whom, how much).
- **Money formatting** (`money.go`): Formats amounts with proper currency symbols and thousands separators (CHF uses apostrophe: `1'234.50`; others use commas/spaces).

### app/internal/auth

Authentication and session management:

- `NewManager()` — creates auth manager with store
- `Login(email, password)` — checks bcrypt hash, creates session
- `Signup(email, password, name)` — creates user, hashes password
- `FromContext(r *http.Request)` — extracts session from cookie
- Sessions are HTTP-only, SameSite=Strict cookies stored in the `sessions` table (expires after 30 days)

### app/internal/fx

Exchange rate provider:

- `NewProvider(store)` — creates rate fetcher
- `Rate(base, quote)` — fetches cached rate, or calls frankfurter.app if stale (>1 day old)
- `RateAt(base, quote, time)` — same but for a specific time (used in snapshots)

Rates are cached in the `fx_rates` table. Falls back to cached rates if API is unavailable.

### app/internal/gold

Gold spot price provider:

- `NewProvider(store, fx)` — creates gold price fetcher
- `PricePerGramCents(currency)` — price in the given currency (fetches from gold-api.com, converts via FX, cached)

Prices are cached in the `gold_prices` table. Manual override available via household settings.

### app/templates

Go `html/template` files:

- `layouts/base.html` — full HTML shell, includes SEO/OG meta tags, scripts, stylesheets
- `sections/` — full pages, e.g. `sections/dashboard.html`, `sections/expenses.html`
- `partials/` — reusable fragments, e.g. `partials/transfer-table.html`, `partials/expense-row.html`

Templates use Go template syntax with custom functions: `{{ money .Amount .Currency }}` → formatted string. All user data is auto-escaped for XSS protection.

### app/static

Embedded assets (served from FS, not disk):

- `css/app.min.css` — compiled Tailwind output (committed)
- `js/htmx.min.js`, `js/alpine.min.js`, `js/chart.umd.js`, `js/chart-sankey.min.js` — vendored libraries (committed)
- `fonts/` — self-hosted woff2 fonts (Space Grotesk, Inter)
- `manifest.json` — PWA manifest
- `sw.js` — service worker for offline viewing
- `og-image.png` — OpenGraph preview image
- Favicons (16x16, 32x32, 180x180)

## Database schema

Every table of user data is scoped to a household, and every query that reads or
writes it carries that scope in the SQL. See
[multi-tenancy.md](multi-tenancy.md) for the rule new store methods must follow,
and for what is still outstanding before hosting more than one household.

All stored in SQLite. Key tables:

| Table | Purpose |
|-------|---------|
| `households` | Top-level entity, includes split method, display currency, invite code |
| `users` | Two per household, email + bcrypt password, equal permissions |
| `sessions` | HTTP-only session tokens, expires after 30 days |
| `income_sources` | Per-user income (type: fixed/variable, pay structure: 12/13, gross yearly, currency) |
| `income_deductions` | Named deductions per income source: a fixed amount (monthly or yearly) or a percentage of gross, stored as basis points |
| `expenses` | Monthly recurring costs (amount, currency, category: personal/common, subcategory, kind: expense/savings/investment/pension) |
| `accounts` | Bank accounts (name, institution, currency, balance, purpose: checking/savings/investment/cc_buffer/envelope/pension) |
| `cc_transactions` | Credit card spends (amount, cashback, used to track CC buffer) |
| `gold_items` | Physical gold (weight grams, purity karat, quantity, location) |
| `assets` | Real estate and other non-liquid assets (name, estimated value, currency) |
| `goals` | Financial targets (amount, currency, deadline, category) |
| `scenarios` | Named what-if configurations |
| `scenario_changes` | Parameter changes within a scenario (expense/income amount/add/remove, return rate, asset add/remove) |
| `trip_plans` | Named trip budgets (start date, months to save, itemized costs) |
| `trip_items` | Itemized trip costs (name, category, amount, currency) |
| `fx_rates` | Cached exchange rates (base, quote, rate, fetched_at) |
| `gold_prices` | Cached gold spot prices (price per gram cents, currency, fetched_at) |
| `saved_projections` | Named projection assumptions (the projections page's query string, stored verbatim) |
| `net_worth_snapshots` | Daily net worth totals (liquid, alternative, real estate, all in CHF at snapshot time) |
| `transfer_confirmations` | JSON snapshot of last-confirmed transfer table (used to detect changes) |

All amounts are stored as INTEGER cents (no floating-point). Timestamps are TEXT ISO8601 UTC. Foreign keys are enforced. Indexes are created on all join columns.

## Calculation engines

### Split ratio

Given a household's split method and two partners' incomes:

- **50/50**: Each partner pays 50% of common expenses
- **Income-weighted**: Ratio is proportional to net monthly income
  - Net = gross yearly - deductions (fixed amounts normalised to yearly, percentages applied to gross), spread over 12 months for planning
  - If `include_variable_income=0`, exclude variable income from the ratio (but include it in available cash)

Example: Partner A earns CHF 5,000/month, Partner B earns CHF 7,000/month.
- Ratio: 41.67% / 58.33%
- Common expense of CHF 1,200 → A pays CHF 500, B pays CHF 700

### Projection

Forward-projects cash flow, savings, and net worth:

- **Input**: Current household state (incomes, expenses, accounts, assets), return rate assumption (0-10% annual), time horizon (1-20 years)
- **Calculation**: For each month:
  1. Sum net income (all sources, after deductions, accounting for 12/13 pay structure)
  2. Subtract all expenses
  3. Remainder is savings (added to relevant account)
  4. Apply monthly return on account balances (annual rate / 12)
  5. Calculate net worth: liquid (accounts) + alternative (gold) + real estate (assets)
- **Output**: Time series of monthly savings, account balances, net worth

### Scenario

Takes a named scenario's parameter changes (e.g. "expense up CHF 500", "income down CHF 300", "add savings goal CHF 50k") and reapplies them to a base state:

1. Load current household state
2. Apply each change: update an expense amount, remove an expense, add an income, etc.
3. Recalculate: split ratio, transfers, available cash, projections
4. Compare side-by-side: "Current" vs "Scenario X"

### Transfer calculator

Produces the monthly transfer table:

1. For each common expense, calculate each partner's share using the split ratio
2. Sum shares per partner
3. Determine target account (common checking for most, common savings for savings expenses)
4. Output: rows like "Partner A → Common Checking: CHF X,XXX"

Each transfer references the expense that drives it, so renaming an expense keeps transfers clear.

## External data

### Exchange rates (frankfurter.app)

API: `https://api.frankfurter.app/latest?base=CHF&symbols=EUR,USD,GBP,TRY`

Response:

```json
{
  "base": "CHF",
  "date": "2024-08-10",
  "rates": {
    "EUR": 0.95,
    "USD": 1.08,
    "GBP": 0.86,
    "TRY": 30.50
  }
}
```

Cached in `fx_rates` table. Refresh once per day (max). Manual refresh button in Settings.

### Gold spot price (gold-api.com)

API: `https://api.gold-api.com/price/XAU`

Response:

```json
{
  "price_gram_usd": 65.50
}
```

Converted to other currencies using FX rates. Purity multiplier applied: 24K = 1.0, 22K = 0.9167, 18K = 0.75, 14K = 0.5833.

Cached in `gold_prices` table. Refresh once per day (max). Manual override in Settings.

## Testing

### Engine tests

`internal/service/*_test.go` — table-driven tests for split calculations, projections, scenario math. These are thorough because they verify the core financial logic.

Example:

```go
func TestSplitRatio_IncomeWeighted(t *testing.T) {
    tests := []struct {
        name string
        income1, income2 int64
        expected1, expected2 float64
    }{
        {"equal", 6000_00, 6000_00, 0.5, 0.5},
        {"60/40", 6000_00, 4000_00, 0.6, 0.4},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

### Handler smoke tests

`internal/handler/*_test.go` — basic HTTP tests: login → dashboard returns 200, POST expense recalculates correctly.

### CI

On PR and push to main: `go vet ./...`, `go test ./...`, `CGO_ENABLED=0 go build ./...` (ensures no CGO dependencies).

## Deployment

See [docs/self-hosting.md](self-hosting.md).

Single Docker image: `ghcr.io/mattmezza/paripari:<version>` or `:latest`. Includes the Go binary, embedded templates, static assets. No external dependencies except the free FX and gold APIs.

Environment variables set the port, data directory, base URL, cookie security level, and dev mode.

## Development

See README.md for build and run instructions.

Key commands:

```bash
make dev      # Hot reload with air
make test     # Run tests
make build    # Build binary
make css      # Rebuild Tailwind CSS
make vendor   # Download htmx, Alpine, Chart.js
make release  # Tag and publish release
```

Edit templates or Go code, and (in dev mode) the app reloads and serves changes.
