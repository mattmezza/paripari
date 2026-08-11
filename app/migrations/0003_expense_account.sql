-- Budget-holder accounts: an expense can name the account its contributions are
-- accumulated in (e.g. a "Groceries budget" envelope). NULL means "use the
-- household default": common savings for savings-tagged expenses, common
-- checking for everything else.
ALTER TABLE expenses ADD COLUMN account_id INTEGER NULL REFERENCES accounts(id) ON DELETE SET NULL;
CREATE INDEX idx_expenses_account ON expenses(account_id);
