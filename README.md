# PariPari

Household finance planning for couples, on equal terms.

PariPari is a self-hosted, open-source app for couples who share expenses. Define your incomes and recurring costs, choose a split method (50/50 or income-weighted), and watch the app calculate everything forward: who contributes what, how much you're saving, when you'll hit goals, and what changes if you adjust any variable.

This is **not** a transaction tracker—no bank imports, no receipt scanning, no purchase categorization. Think of it as a flight simulator for household finances: change a parameter, see the ripple across monthly cash flow, contribution splits, and net worth trajectory instantly.

## What it is

- Financial planning and projection for two-partner households
- Configurable expense splits (50/50 or income-weighted, changeable at any time)
- Monthly contribution breakdown: exactly what each partner transfers to shared accounts
- Goals and timelines: when will you reach your targets at the current saving rate?
- What-if scenarios: stack parameter changes (salary raises, expense cuts, new costs) and see the cascade effect
- Multi-currency support with live exchange rates (CHF, EUR, TRY, USD, GBP, more)
- Gold tracking: auto-valued with live spot prices, purity-aware
- Net worth overview: liquid + alternative assets + real estate, in any currency
- Trip budgeting: itemize costs, see impact on goal timelines, decide if it's worth it
- PWA: installable on mobile, works offline for viewing data

## What it is not

- A transaction tracker (no bank imports, no receipt scanning)
- A budgeting app (no category-per-purchase tracking)
- A tax tool
- US-centric (supports multiple currencies and locales from the start)

## Features

- **Split methods**: 50/50 or income-weighted (with optional variable income inclusion)
- **Transfer instructions**: monthly table showing exactly what each partner should transfer
- **Savings projection**: compound-growth modeling, goal timelines
- **Scenario engine**: create named what-if configurations, compare side-by-side
- **Multi-currency**: view everything in CHF, EUR, TRY, USD, GBP, etc.
- **Gold inventory**: weight, purity, quantity, auto-valued with live prices
- **Net worth tracking**: liquid, alternative (gold), real estate, total
- **Trip planning**: itemized budget, impact analysis
- **Full transparency**: both partners see and edit everything, no admin/viewer roles

## Quick start

### With Docker Compose

```bash
docker compose up -d
```

Visit `http://localhost:8080`, sign up with any email, and note the invite code shown in settings. Share that code with your partner so they can sign up and join the household.

### Environment variables

Set these in your `.env` file or pass to Docker:

| Variable | Default | Notes |
|----------|---------|-------|
| `PORT` | `8080` | HTTP port |
| `DATA_DIR` | `./data` | Directory for SQLite database and WAL file |
| `BASE_URL` | `http://localhost:8080` | Public URL of the app (used for OG meta tags, redirects) |
| `SECURE_COOKIES` | `0` | Set to `1` when behind TLS (required for production) |
| `DEV` | `0` | Set to `1` to reload templates from disk (development only) |

### First run

1. Open the app in your browser
2. Sign up: pick any email and password
3. You're now in your household; check **Settings** for the invite code
4. Share the code with your partner
5. Your partner signs up with the same code to join your household

## Configuration

All settings are configured via the first-run signup flow and the app UI. No config files needed.

- **Split method**: Choose 50/50 or income-weighted under Household Settings
- **Display currency**: Switch between CHF, EUR, TRY, USD, GBP from any page
- **Exchange rates**: Fetched automatically once per day (manual refresh in Settings)
- **Gold prices**: Auto-updated daily; set a manual override in Settings if needed

## Development

### Prerequisites

- Go 1.20+ (see `app/go.mod` for the exact version)
- Make

### Build and run

```bash
# Download tools and dependencies
make tools
make vendor

# Build CSS (needs make tools)
make css

# Run locally with hot reload (requires air installed)
make dev

# Run tests
make test

# Build a static binary
make build

# Seed demo data (test database only)
make seed

# Build Docker image locally
make docker-build
```

See `make help` for all targets.

## Release process

Tag and publish a release:

```bash
make release name=v0.1
```

The target refuses to run on a dirty tree, with unpushed commits, or on a tag
that already exists; it runs the tests, then tags, pushes, and creates the
GitHub release. Publishing the release triggers the workflow that builds the
Docker image and pushes `ghcr.io/mattmezza/paripari:v0.1` and `:latest`.

CI runs on pull requests only. Nothing is built or published on a plain commit
or a push to `main` — the release workflow re-runs vet and tests before it
builds, so a tag cut from a broken tree never reaches the registry.

Both workflows run on a self-hosted runner; make sure it is active before
opening a PR or releasing.

## Tech stack

- **Backend**: Go, SQLite (pure Go driver, no CGO needed)
- **Frontend**: htmx, Alpine.js, Tailwind CSS v4, Chart.js
- **Deployment**: Docker, single container with embedded assets
- **PWA**: manifest.json, service worker, offline-capable

## License

MIT. See LICENSE file. Copyright 2026 Matteo Merola.

## Contributing

Open an issue or pull request. Larger changes: open an issue first to discuss.

---

For detailed deployment, architecture, and self-hosting information, see the [docs/](docs) directory.
