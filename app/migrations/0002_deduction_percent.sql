-- Deductions can be a percentage of gross yearly income (AHV, pension, tax at
-- source are all quoted as rates). SQLite can't widen a CHECK constraint in
-- place, so the table is rebuilt.
CREATE TABLE income_deductions_new (
    id               INTEGER PRIMARY KEY,
    income_source_id INTEGER NOT NULL REFERENCES income_sources(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    amount_cents     INTEGER NOT NULL CHECK (amount_cents >= 0),
    period           TEXT NOT NULL CHECK (period IN ('monthly','yearly','percent')),
    -- Basis points of gross yearly, so 5.30% stores as 530. Only read when
    -- period = 'percent'; amount_cents is 0 in that case.
    percent_bp       INTEGER NOT NULL DEFAULT 0 CHECK (percent_bp >= 0)
);

INSERT INTO income_deductions_new (id, income_source_id, name, amount_cents, period, percent_bp)
SELECT id, income_source_id, name, amount_cents, period, 0 FROM income_deductions;

DROP TABLE income_deductions;
ALTER TABLE income_deductions_new RENAME TO income_deductions;
CREATE INDEX idx_deductions_source ON income_deductions(income_source_id);
