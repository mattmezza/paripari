-- A saved projection is a name plus the projections page's own query string.
-- Every assumption the page models -- horizon, return rate, gold rate, which
-- pieces of the starting balance count, cash held, inflation, goal spending,
-- one-off expenses, scenario overlays -- already travels in the URL, so loading
-- a saved one is nothing more than /projections?<params>.
--
-- One TEXT column instead of a column per knob: the knob set grows every time
-- the page learns to model something new, and a mirrored schema would need a
-- migration each time. A param a newer version stops understanding is simply
-- ignored by the parser instead of leaving a dead column behind.
CREATE TABLE saved_projections (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    params       TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_saved_projections_household ON saved_projections(household_id);
