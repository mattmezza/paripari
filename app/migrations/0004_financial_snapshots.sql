-- The monthly cash-flow picture as it stood on a given date. Expenses here are
-- a recurring monthly model, not observed spend: one row per household per day
-- describing "what the monthly picture looked like that day". All amounts
-- CHF-converted at snapshot time, like net_worth_snapshots.
CREATE TABLE financial_snapshots (
    id                      INTEGER PRIMARY KEY,
    household_id            INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    date                    TEXT NOT NULL,
    currency                TEXT NOT NULL DEFAULT 'CHF',
    income_cents            INTEGER NOT NULL DEFAULT 0,
    expenses_cents          INTEGER NOT NULL DEFAULT 0,
    savings_cents           INTEGER NOT NULL DEFAULT 0,
    available_cents         INTEGER NOT NULL DEFAULT 0,
    common_expenses_cents   INTEGER NOT NULL DEFAULT 0,
    personal_expenses_cents INTEGER NOT NULL DEFAULT 0,
    UNIQUE (household_id, date)
);
