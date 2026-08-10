# PariPari — Build Prompt

## What This Is

**PariPari** (paripari.app) is a personal finance **planning and projection** app for couples who share expenses. This is NOT a transaction tracker. There are no bank imports, no receipt scanning, no categorization of individual purchases. Instead, you define your incomes and recurring expenses, and the app calculates everything forward: how much each partner contributes, what you're saving, when you'll hit goals, and what happens if you change any variable.

The name comes from the Italian *pari* / *alla pari* — "equal," "on equal terms." That's the thesis: two partners, full transparency, a fair split, one shared picture.

Think of it as a **flight simulator for household finances** — change a parameter, see the ripple across monthly cash flow, contribution splits, goal timelines, and net worth trajectory instantly.

The closest existing product is ProjectionLab, but PariPari is open source, self-hosted, not US-centric, and built around a specific workflow: two partners with shared-but-separate finances, contributing to common expenses based on a configurable split method.

---

## Core Concepts

### Household

A household is the top-level entity. It contains exactly two partner accounts with identical permissions. Both partners can see and edit everything. There is no admin/viewer distinction — full transparency is the foundation.

### Partners & Authentication

Two user accounts per household, each with their own login. Simple auth (email + password or passkey). The app is designed to be self-hosted, so no complex auth provider needed — but keep it clean and secure.

### Income

Each partner defines their income sources. Income has these properties:

- **Type**: `fixed` (employment salary) or `variable` (side hustle, freelance, venture)
- **Pay structure**: 12 or 13 monthly installments (some employers pay in 13)
- **Gross yearly amount**
- **Deductions**: A list of named deductions (taxes, social insurance, pension contributions, etc.) each with a monthly or yearly amount
- **Net monthly**: Calculated from gross, deductions, and pay structure
- **Currency**: The currency this income is paid in

### Split Method

The household can choose between two split methods for common expenses:

1. **50/50** — each partner pays half of all common expenses regardless of income
2. **Income-weighted** — each partner's share is proportional to their net income. Example: if Partner A earns CHF 8,954/month and Partner B earns CHF 11,179/month, the ratio is 44.49% / 55.51%

This is a household-level setting that can be changed at any time. When set to income-weighted, the user can choose whether variable income (side hustles, freelance) is included in or excluded from the ratio calculation. Changing the split method instantly recalculates all contribution amounts.

### Expenses

Expenses are recurring monthly costs. Each expense has:

- **Name**
- **Amount**
- **Currency**
- **Category**: `personal` (belongs to one partner) or `common` (shared)
- **Subcategory/tag**: e.g. rent, utilities, insurance, transport, subscriptions, childcare, property maintenance
- If `common`: the split is automatically calculated using the household's chosen split method
- If `personal`: it's fully assigned to that partner

**Savings are modeled as expenses** — a monthly savings amount is just a common or personal "expense" tagged as savings. This keeps the math unified.

Expenses should be trivially editable. Changing a single expense amount should instantly recalculate everything downstream: the partner contributions, the monthly available amount, goal timelines, projections.

### Accounts

Bank accounts are modeled as **purpose-dedicated containers**, not just bank connections. Examples:

- Common checking (for rent, utilities)
- Common savings
- Holiday budget
- Groceries budget
- Credit card buffer
- Personal savings Partner A
- Personal savings Partner B
- Investment account
- 3rd pillar / pension
- Property-related

Each account has: name, bank/institution, currency, current balance, and purpose/type (checking, savings, investment, credit card buffer, budget envelope).

### Credit Card Workflow

A dedicated pattern for credit card discipline:

- A specific account is designated as the CC buffer
- When you spend on the credit card, you immediately set aside that amount in the buffer
- The buffer balance should always cover the upcoming CC bill
- Track cashback earned
- This is a first-class workflow, not a hack — the app should make it easy to record a CC spend and see the buffer adjust

### Holiday / Trip Budgeting

Plan trips by itemizing costs (flights, accommodation, activities, meals, transport). Save these as named plans. Each plan shows:

- Total estimated cost
- Impact on monthly available cash (if saving for it over N months)
- Impact on goal timelines
- Comparison: "with this trip" vs "without this trip"

Plans should be saveable and trackable — you can create a plan, see its impact, decide to commit, and then track progress toward funding it.

### Consolidated Transfer Instructions

The key operational output. Based on all common expenses and the chosen split method, the app calculates exactly how much each partner needs to transfer to the common account(s) each month. This is displayed as a transfer table:

| From | To | Amount | Reference |
|------|-----|--------|-----------|
| Partner A personal | Common checking | CHF X,XXX | Monthly contribution |
| Partner A personal | Common savings | CHF XXX | Savings contribution |

These transfers are the reference for configuring standing orders at the bank. When any expense changes, the app should **flag that transfer amounts have changed** and show the new amounts so partners can update their standing orders.

### Multi-Currency

The app operates in multiple currencies (CHF, EUR, TRY, and potentially others). Each income, expense, account, and asset is stored in its native currency.

- **FX rates** are fetched automatically and kept current
- A **global currency switcher** lets you view the entire app — dashboard, projections, net worth — in any supported currency
- Projections and net worth calculations convert everything to the selected display currency

### Gold Tracking

A dedicated section for physical gold assets. Gold items are defined by:

- **Weight** in grams
- **Purity** (24K, 22K, 18K, 14K, etc.)
- **Location** (e.g. bank safe, home)
- **Quantity**

The app fetches the **gold spot price** automatically, calculates the value of each item based on weight × purity × spot price, and converts to the display currency using live FX rates. No Turkish gold naming conventions — everything is grams and karat purity.

### Net Worth Tracking

A consolidated view of all assets, broken down into:

- **Liquid**: Cash in all accounts + investment portfolio value
- **Alternative**: Gold (auto-valued)
- **Real estate**: Properties with manually-set estimated market values
- **Total net worth**: All of the above

Show both "liquid only" and "total including property" views. Each asset/account links to its currency and is converted to display currency for the total.

### Savings Projection Engine

This is where the app earns its keep. Based on current monthly saving power (income minus all expenses), project forward:

- **With compound growth**: Model different annual return rates (0% for cash, 4-7% for investments, etc.)
- **Goal timelines**: "At this rate, you'll reach Goal X in Y months"
- **Side-by-side comparison**: Show multiple scenarios simultaneously

### What-If Scenario Engine

The killer feature. Create named scenarios by stacking parameter changes:

- Change an expense amount (rent goes up CHF 500)
- Change an income (Partner A gets a raise of CHF 500/month)
- Add a new expense (new car lease at CHF 400/month)
- Remove an expense (pay off mortgage)
- Change an investment return rate
- Add or sell an asset

Each scenario shows the **cascade effect** across:

1. Monthly cash flow (how much is left)
2. The contribution split (ratio may change if income changes)
3. Transfer amounts
4. Goal timelines (how much faster/slower you reach each goal)
5. Net worth projection over 1, 5, 10, 20 years

You should be able to compare "Current" vs "Scenario A" vs "Scenario B" side by side, with clear visual diffs — charts overlaying the projection lines, delta numbers highlighted.

### Goals

Named financial goals with:

- Target amount
- Target currency
- Optional deadline
- Category (e.g. safety net, property purchase, education fund)
- Current progress (calculated from relevant account balances)
- Projected completion date (calculated from saving power and return assumptions)

---

## Technical Architecture

### Stack

- **Go** (latest stable) — backend, server-side rendering
- **SQLite** — single-file database, no external DB dependency
- **htmx** — server-driven interactivity, partial page updates
- **Alpine.js** — client-side reactivity for charts, toggles, modals
- **Tailwind CSS** — utility-first styling
- **Docker** — single container deployment
- **PWA** — installable, mobile-first, works offline for viewing (online for FX/gold price updates)

### Project Structure

```
/
├── app/                    # Main application
│   ├── cmd/                # Entry points
│   │   └── server/
│   │       └── main.go
│   ├── internal/
│   │   ├── handler/        # HTTP handlers (htmx endpoints)
│   │   ├── model/          # Domain types
│   │   ├── store/          # SQLite repository layer
│   │   ├── service/        # Business logic (projection engine, split calculator, FX, gold pricing)
│   │   ├── fx/             # FX rate fetcher
│   │   ├── gold/           # Gold spot price fetcher
│   │   └── auth/           # Authentication
│   ├── migrations/         # SQL migrations
│   ├── templates/          # Go html/template files (htmx partials + full pages)
│   │   └── layouts/        # Base layout with SEO/OG meta tags
│   ├── static/             # CSS, JS, icons, manifest.json, og-image.png
│   └── Dockerfile
├── www/                    # Public marketing website (future)
├── docs/                   # Documentation
├── Makefile
├── .github/
│   └── workflows/
│       ├── ci.yml          # Run tests on PR
│       └── release.yml     # Build + push to GHCR on release
└── README.md
```

Go module path: `github.com/mattmezza/paripari`
Docker image: `ghcr.io/mattmezza/paripari`

### Makefile

```makefile
make help              # List all targets
make dev               # Run locally with hot reload
make test              # Run all tests
make build             # Build Go binary
make docker-build      # Build Docker image locally
make release name=v0.1 # Create GitHub release → triggers GHA → builds Docker image → pushes to ghcr.io
```

- **Regular commits**: no build, no push
- **PRs**: CI runs tests only
- **Releases** (`make release name=vX.Y`): creates a GitHub release tag, which triggers GHA to build the Docker image and push to GHCR
- **GHA uses a self-hosted runner**

### Database Schema (SQLite)

Design the schema to support all the concepts above. Key tables:

- `households` — top-level entity, includes split_method (fifty_fifty | income_weighted), include_variable_income_in_ratio (boolean)
- `users` — two per household, equal permissions
- `income_sources` — per user, with type (fixed/variable), pay_structure (12/13), gross, currency
- `income_deductions` — per income source
- `expenses` — name, amount, currency, category (personal/common), assigned_user (if personal), subcategory
- `accounts` — name, institution, currency, balance, type/purpose, household_id
- `gold_items` — weight_grams, purity_karat, quantity, location, household_id
- `assets` — for real estate and other non-liquid assets, with name, estimated_value, currency
- `goals` — name, target_amount, currency, deadline, category
- `scenarios` — named what-if configurations
- `scenario_changes` — individual parameter changes within a scenario (references the thing being changed + the new value)
- `trip_plans` — named trip budgets with itemized costs
- `fx_rates` — cached exchange rates with timestamp
- `gold_prices` — cached gold spot prices with timestamp

Use migrations (golang-migrate or similar). The schema should be clean and normalized.

### API / Routing

This is an htmx-driven app, so routes return HTML partials. Structure routes logically:

- `GET /` — dashboard
- `GET /income` — income management
- `GET /expenses` — expense management
- `GET /accounts` — account overview
- `GET /transfers` — consolidated transfer instructions
- `GET /net-worth` — net worth breakdown
- `GET /gold` — gold inventory
- `GET /goals` — goals and progress
- `GET /scenarios` — what-if scenario builder
- `GET /trips` — trip planning
- `GET /projections` — savings/net worth projections
- `GET /settings` — household settings, FX, preferences

Each section should support htmx partial updates — editing an expense inline should recalculate and re-render the affected parts of the page (split amounts, available cash, transfer table) without a full page reload.

---

## SEO & Open Graph

Implement full SEO and Open Graph metadata in the base HTML layout. This matters for the public-facing aspects (login page, marketing, link sharing) and for when the app is eventually open-sourced.

### Base Layout Meta Tags

Every page should include in the `<head>`:

```html
<!-- Primary Meta Tags -->
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{ .PageTitle }} — PariPari</title>
<meta name="description" content="{{ .MetaDescription }}">
<meta name="theme-color" content="{{ .ThemeColor }}">
<link rel="canonical" href="{{ .CanonicalURL }}">

<!-- Open Graph -->
<meta property="og:type" content="website">
<meta property="og:url" content="{{ .CanonicalURL }}">
<meta property="og:title" content="{{ .PageTitle }} — PariPari">
<meta property="og:description" content="{{ .MetaDescription }}">
<meta property="og:image" content="{{ .BaseURL }}/static/og-image.png">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta property="og:site_name" content="PariPari">
<meta property="og:locale" content="en_US">

<!-- Twitter Card -->
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="{{ .PageTitle }} — PariPari">
<meta name="twitter:description" content="{{ .MetaDescription }}">
<meta name="twitter:image" content="{{ .BaseURL }}/static/og-image.png">

<!-- PWA -->
<link rel="manifest" href="/static/manifest.json">
<link rel="icon" type="image/png" sizes="32x32" href="/static/favicon-32x32.png">
<link rel="icon" type="image/png" sizes="16x16" href="/static/favicon-16x16.png">
<link rel="apple-touch-icon" sizes="180x180" href="/static/apple-touch-icon.png">
```

### Default Meta Content

- **Title**: "PariPari — Household Finance Planning for Couples"
- **Description**: "Plan your household finances together, on equal terms. Track income, expenses, and savings with configurable splits, what-if scenarios, multi-currency support, and forward projections."
- **OG Image**: Create a clean, branded 1200×630 image with the PariPari wordmark, a brief tagline, and a subtle visual hint of the app (e.g. a simplified split visualization or projection curve). Use the app's color palette.

### Per-Page Overrides

The base layout should accept per-page title and description overrides via Go template variables. Authenticated app pages can use generic descriptions since they won't be publicly indexed, but the login page and any future public pages should have proper unique meta content.

### robots.txt and sitemap

- Serve a `robots.txt` that allows indexing of public pages (login, marketing) but disallows authenticated app routes
- Include a basic `sitemap.xml` for public pages

### Structured Data (JSON-LD)

Add basic JSON-LD structured data on public pages:

```json
{
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  "name": "PariPari",
  "applicationCategory": "FinanceApplication",
  "operatingSystem": "Web",
  "description": "Household finance planning for couples — income, expenses, configurable splits, projections, and what-if scenarios.",
  "offers": {
    "@type": "Offer",
    "price": "0",
    "priceCurrency": "USD"
  }
}
```

---

## Design Requirements

### Philosophy

Design is not decoration — it's how the app works. Every screen should feel intentional, calm, and clear. This is a tool for making important life decisions with money; it should feel trustworthy and precise, like a well-made instrument.

The name carries a design idea: *pari* means equal. Where the UI shows the two partners, it should feel balanced — paired, symmetric, neither one subordinate to the other. Lean into that visually where it makes sense (the split visualization, the transfer table, the dashboard's per-partner columns).

### Mobile-First PWA

- **Primary use case is on a phone.** Design for ~380px viewport first, then scale up.
- Installable as PWA with proper manifest, icons, and service worker for offline viewing.
- Touch-friendly targets, swipeable where appropriate, no hover-dependent interactions.
- The dashboard should be immediately useful on a phone — no scrolling past a hero section to get to the numbers.

### Visual Direction

- Clean, modern, with generous whitespace and strong typography
- A **restrained color palette** — no rainbow dashboards. Use color with purpose: one accent for positive/growth, one for warnings/attention, neutrals for everything else
- **Charts should be beautiful and engaging** — smooth animations, clear labels, thoughtful use of color. Consider Chart.js or a similar library. The projection charts (especially scenario comparisons with overlapping lines) are the visual centerpiece
- Numbers should be formatted clearly: thousands separators, currency symbols, consistent decimal places
- The what-if scenario comparison should feel like a before/after reveal — make the impact of changes visually obvious and satisfying
- Subtle transitions when values recalculate (e.g. a number ticking to its new value when you change an expense)
- Dark mode support

### Key Screens

1. **Dashboard**: Monthly overview — income, total expenses, savings, available cash per partner. Contribution split visualization. Quick net worth summary. Goal progress bars. Any flags ("transfer amounts changed").

2. **Expense Editor**: List of all expenses (common + personal), inline editable. Changing any value instantly shows the impact: updated contribution amounts, updated available cash. Group by subcategory.

3. **Transfer Table**: The "what to do" screen. Clean table of transfers with copy-to-clipboard for IBANs and amounts. Highlighted if amounts differ from last confirmed state.

4. **Projections**: Time-series charts showing net worth and/or savings growth over time. Toggle between scenarios. Slider or input for return rate assumptions. Goal markers on the timeline.

5. **Scenario Builder**: Create and name scenarios. Add changes (adjust expense, add income, remove expense, etc.). See side-by-side impact across all metrics. Save and compare multiple scenarios.

6. **Net Worth**: Breakdown chart (liquid / alternative / real estate). Historical trend. Currency switcher.

7. **Gold**: Inventory table with auto-calculated values. Total weight, total value in display currency.

8. **Trip Planner**: Itemized cost breakdown, total, monthly saving needed, impact on goals.

---

## External Data

### FX Rates

Fetch from a free API (e.g. frankfurter.app, exchangerate.host, or ECB feed). Cache in SQLite with a timestamp. Refresh at most once per day (or on manual request). Support at minimum: CHF, EUR, TRY, USD, GBP.

### Gold Spot Price

Fetch from a free API or scrape a reliable source. Cache similarly. Need the price per gram in USD or EUR, then convert using FX rates. Apply purity multiplier (24K = 100%, 22K = 91.67%, 18K = 75%, 14K = 58.33%).

---

## What Success Looks Like

When PariPari is running, a user should be able to:

1. Log in, see their monthly financial picture at a glance
2. Change any single expense and instantly see how it affects their contribution, savings, and goals
3. Create a scenario: "What if I get a CHF 500 raise AND we move to a CHF 500/month more expensive apartment AND we switch schools saving CHF 200/month?" — and see the net effect across everything
4. Check the transfer table, see if amounts need updating, copy the new numbers
5. View their net worth in CHF, switch to EUR, see it in EUR
6. See their gold portfolio valued at today's price without looking anything up
7. Plan a holiday, see how it delays their savings goals, decide if it's worth it
8. Look at a 10-year projection with compound growth and feel confident about their financial path
9. Switch the split method between 50/50 and income-weighted and watch every number update
10. Share a link and see a proper OG preview card with PariPari branding

All of this should feel fast, beautiful, and work perfectly on a phone.

---

## Implementation Notes

- Start with the data model and migrations
- Build the core calculation engine (split calculator, projection engine) as pure Go functions with tests
- Then build the UI screen by screen, starting with the dashboard
- Charts can use Chart.js loaded via CDN, initialized with Alpine.js
- For htmx partial updates: when an expense is edited, the handler should recalculate all affected values and return the updated HTML fragments
- The projection/scenario engine is the most complex part — invest time in getting the math right and making it testable
- Keep the Docker image lean — single static binary + templates + static assets
- PWA manifest and service worker should be set up from the start, not bolted on later
- SEO meta tags and OG image should be in the base layout from day one — not an afterthought
