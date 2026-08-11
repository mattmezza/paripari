-- PariPari initial schema.
-- Money: INTEGER minor units (cents). Timestamps: TEXT ISO8601 UTC (strftime('%Y-%m-%dT%H:%M:%SZ','now')).

CREATE TABLE households (
    id                      INTEGER PRIMARY KEY,
    name                    TEXT NOT NULL,
    split_method            TEXT NOT NULL DEFAULT 'fifty_fifty'
                                CHECK (split_method IN ('fifty_fifty','income_weighted')),
    include_variable_income INTEGER NOT NULL DEFAULT 1 CHECK (include_variable_income IN (0,1)),
    display_currency        TEXT NOT NULL DEFAULT 'CHF',
    manual_gold_price_cents INTEGER,
    invite_code             TEXT NOT NULL UNIQUE,
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    household_id  INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_users_household ON users(household_id);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE income_sources (
    id                 INTEGER PRIMARY KEY,
    household_id       INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    kind               TEXT NOT NULL CHECK (kind IN ('fixed','variable')),
    pay_structure      INTEGER NOT NULL DEFAULT 12 CHECK (pay_structure IN (12,13)),
    gross_yearly_cents INTEGER NOT NULL CHECK (gross_yearly_cents >= 0),
    currency           TEXT NOT NULL,
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_income_household ON income_sources(household_id);
CREATE INDEX idx_income_user ON income_sources(user_id);

CREATE TABLE income_deductions (
    id               INTEGER PRIMARY KEY,
    income_source_id INTEGER NOT NULL REFERENCES income_sources(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    amount_cents     INTEGER NOT NULL CHECK (amount_cents >= 0),
    period           TEXT NOT NULL CHECK (period IN ('monthly','yearly'))
);
CREATE INDEX idx_deductions_source ON income_deductions(income_source_id);

CREATE TABLE expenses (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0),
    currency     TEXT NOT NULL,
    category     TEXT NOT NULL CHECK (category IN ('personal','common')),
    user_id      INTEGER REFERENCES users(id) ON DELETE CASCADE,
    subcategory  TEXT NOT NULL DEFAULT '',
    is_savings   INTEGER NOT NULL DEFAULT 0 CHECK (is_savings IN (0,1)),
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    CHECK ((category = 'personal' AND user_id IS NOT NULL) OR (category = 'common' AND user_id IS NULL))
);
CREATE INDEX idx_expenses_household ON expenses(household_id);

CREATE TABLE accounts (
    id            INTEGER PRIMARY KEY,
    household_id  INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id       INTEGER REFERENCES users(id) ON DELETE SET NULL,
    name          TEXT NOT NULL,
    institution   TEXT NOT NULL DEFAULT '',
    iban          TEXT NOT NULL DEFAULT '',
    currency      TEXT NOT NULL,
    balance_cents INTEGER NOT NULL DEFAULT 0,
    purpose       TEXT NOT NULL CHECK (purpose IN ('checking','savings','investment','cc_buffer','envelope','pension')),
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_accounts_household ON accounts(household_id);

CREATE TABLE cc_transactions (
    id             INTEGER PRIMARY KEY,
    account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    description    TEXT NOT NULL,
    amount_cents   INTEGER NOT NULL,
    currency       TEXT NOT NULL,
    cashback_cents INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_cc_account ON cc_transactions(account_id);

CREATE TABLE gold_items (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    weight_grams REAL NOT NULL CHECK (weight_grams > 0),
    purity_karat INTEGER NOT NULL CHECK (purity_karat BETWEEN 1 AND 24),
    quantity     INTEGER NOT NULL DEFAULT 1 CHECK (quantity > 0),
    location     TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_gold_household ON gold_items(household_id);

CREATE TABLE assets (
    id                   INTEGER PRIMARY KEY,
    household_id         INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    kind                 TEXT NOT NULL DEFAULT 'real_estate' CHECK (kind IN ('real_estate','other')),
    estimated_value_cents INTEGER NOT NULL DEFAULT 0,
    currency             TEXT NOT NULL,
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_assets_household ON assets(household_id);

CREATE TABLE goals (
    id                  INTEGER PRIMARY KEY,
    household_id        INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    target_amount_cents INTEGER NOT NULL CHECK (target_amount_cents > 0),
    currency            TEXT NOT NULL,
    deadline            TEXT,
    category            TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_goals_household ON goals(household_id);

CREATE TABLE scenarios (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_scenarios_household ON scenarios(household_id);

CREATE TABLE scenario_changes (
    id          INTEGER PRIMARY KEY,
    scenario_id INTEGER NOT NULL REFERENCES scenarios(id) ON DELETE CASCADE,
    change_type TEXT NOT NULL CHECK (change_type IN
                    ('expense_amount','expense_add','expense_remove','income_amount','income_add',
                     'income_remove','return_rate','asset_add','asset_remove')),
    target_id   INTEGER,
    label       TEXT NOT NULL DEFAULT '',
    value_cents INTEGER,
    value_num   REAL,
    value_text  TEXT NOT NULL DEFAULT '',
    currency    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_scenario_changes_scenario ON scenario_changes(scenario_id);

CREATE TABLE trip_plans (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    start_date     TEXT,
    months_to_save INTEGER NOT NULL DEFAULT 1 CHECK (months_to_save > 0),
    committed      INTEGER NOT NULL DEFAULT 0 CHECK (committed IN (0,1)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_trips_household ON trip_plans(household_id);

CREATE TABLE trip_items (
    id           INTEGER PRIMARY KEY,
    trip_plan_id INTEGER NOT NULL REFERENCES trip_plans(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    category     TEXT NOT NULL DEFAULT '',
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0),
    currency     TEXT NOT NULL
);
CREATE INDEX idx_trip_items_plan ON trip_items(trip_plan_id);

CREATE TABLE fx_rates (
    base       TEXT NOT NULL,
    quote      TEXT NOT NULL,
    rate       REAL NOT NULL CHECK (rate > 0),
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (base, quote)
);

CREATE TABLE gold_prices (
    id                    INTEGER PRIMARY KEY,
    price_per_gram_cents  INTEGER NOT NULL CHECK (price_per_gram_cents > 0),
    currency              TEXT NOT NULL,
    fetched_at            TEXT NOT NULL
);
CREATE INDEX idx_gold_prices_fetched ON gold_prices(fetched_at);

-- All amounts CHF-converted at snapshot time.
CREATE TABLE net_worth_snapshots (
    id               INTEGER PRIMARY KEY,
    household_id     INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    date             TEXT NOT NULL,
    liquid_cents     INTEGER NOT NULL DEFAULT 0,
    alternative_cents INTEGER NOT NULL DEFAULT 0,
    real_estate_cents INTEGER NOT NULL DEFAULT 0,
    UNIQUE (household_id, date)
);

-- JSON snapshot of the transfer table as last confirmed by the partners.
CREATE TABLE transfer_confirmations (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    payload      TEXT NOT NULL,
    confirmed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_transfer_conf_household ON transfer_confirmations(household_id, confirmed_at DESC);
