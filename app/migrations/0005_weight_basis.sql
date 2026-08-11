-- The income-weighted split can follow net or gross income. Net is what lands
-- in your account; gross ignores how differently the two partners are taxed.
-- Existing households keep the behaviour they already had, so the default is
-- 'net'.
--
-- A plain ADD COLUMN, not a new split_method value: widening that CHECK would
-- mean rebuilding households, and every other table has a foreign key into it
-- (foreign_keys is ON — see store.Open). The basis is a modifier on the
-- weighting, the same shape as include_variable_income, so it belongs in its
-- own column anyway.
ALTER TABLE households ADD COLUMN weight_basis TEXT NOT NULL DEFAULT 'net'
    CHECK (weight_basis IN ('net','gross'));
