-- An investment or a pension contribution is not a regular expense, and it is
-- not quite plain savings either — the Tag field on an expense now says which
-- of the four it is, instead of a yes/no savings flag.
--
-- One column, not two: is_savings is replaced rather than kept alongside, so
-- there is no pair of fields that can disagree. Everything that is not
-- 'expense' is still treated as savings by every calculation, which is what
-- the old flag meant.
ALTER TABLE expenses ADD COLUMN kind TEXT NOT NULL DEFAULT 'expense'
    CHECK (kind IN ('expense','savings','investment','pension'));

UPDATE expenses SET kind = 'savings' WHERE is_savings = 1;

ALTER TABLE expenses DROP COLUMN is_savings;
