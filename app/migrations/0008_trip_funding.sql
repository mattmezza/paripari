-- A trip plan now says where its money comes from and how it gets there.
--
-- funding_account_id: the account that holds the trip money, usually a budget
-- envelope. NULL keeps today's behaviour -- the household default, i.e. common
-- checking, the same default an expense with no account_id follows.
--
-- funding_strategy: 'spread' is what every existing row does, so it is the
-- default -- put months_to_save aside every month until the trip is paid for.
-- 'one_shot' means the money is already in the account and is spent in one go:
-- nothing recurring is added, the account is simply drawn down.
ALTER TABLE trip_plans ADD COLUMN funding_account_id INTEGER NULL REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE trip_plans ADD COLUMN funding_strategy TEXT NOT NULL DEFAULT 'spread'
    CHECK (funding_strategy IN ('spread','one_shot'));

-- linked_expense_id replaces the "trip:<id>" subcategory that used to tie a
-- committed trip to the expense it created. That trick made the expense read as
-- "trip:7" on /expenses and gave every trip its own one-row group in the expense
-- analysis; committed trips now file under a plain "holidays" subcategory and
-- the link lives in a real column.
ALTER TABLE trip_plans ADD COLUMN linked_expense_id INTEGER NULL REFERENCES expenses(id) ON DELETE SET NULL;

-- Carry the existing links over before the marker they depend on is rewritten.
UPDATE trip_plans SET linked_expense_id = (
    SELECT e.id FROM expenses e
    WHERE e.household_id = trip_plans.household_id
      AND e.subcategory = 'trip:' || trip_plans.id
) WHERE committed = 1;

UPDATE expenses SET subcategory = 'holidays' WHERE subcategory LIKE 'trip:%';
