# Multi-Tenancy

PariPari's data model has always been multi-household: one instance can hold
many households, and the code enforces the boundary between them. What is *not*
yet done is the operational work that makes hosting other people's households
responsible. This document records both halves — how isolation works, and what
is left to do before running this for anyone but yourself.

> **Status:** the isolation is in place and tested. The hosting prerequisites in
> the last section are **not** done. Do not host other households until they are.

## The tenant is the household

Not the user. A household holds at most two users, enforced server-side in
`auth.Signup` via `CountUsers` — not merely hidden in the UI. Every row of user
data hangs off `households.id`, directly or through a parent.

A second user joins an existing household with an 8-character invite code
(`store.NewInviteCode`, `crypto/rand`, ambiguous characters removed). Signing up
with an empty invite field creates a *new* household, which is what makes the
instance multi-tenant in the first place.

## How a request gets scoped

```
cookie pp_session
  → auth.Manager.load  looks the token up in `sessions`
  → *auth.Session{User, Partner, Household} on the request context
  → handler reads sess.Household.ID from auth.FromContext(r)
  → store method takes householdID as its first argument
  → SQL carries `AND household_id = ?`
```

The household id is **never** taken from the client. There is no route where a
request body or query parameter can name a household. `RequireAuth` wraps the
entire authenticated mux in `handler/router.go`, and CSRF is session-bound and
checked on every non-GET — via the `X-CSRF-Token` header that `base.html` sets
through `hx-headers`, a `Sec-Fetch-Site` same-origin fallback, or a hidden form
field.

Session cookies are HttpOnly, SameSite=Strict, 256-bit `crypto/rand` tokens with
a 30-day expiry, stored server-side so they can be revoked, and garbage-collected
daily. `Secure` is gated on the `SECURE_COOKIES` environment variable.

### Top-level entities

Accounts, expenses, income sources, goals, assets, gold items, scenarios and
trip plans all carry `household_id` themselves. Every store method — read *and*
write, including lookups by primary key — appends `AND household_id = ?`:

```go
func (s *Store) Expense(householdID, id int64) (*model.Expense, error) {
    // ... WHERE id = ? AND household_id = ?
}
```

### Child records

Deductions, scenario changes, trip items and card transactions have no
`household_id` column of their own. They are scoped through their parent with a
named predicate constant, appended to every statement that touches them:

```go
const ownedIncome   = ` AND income_source_id IN (SELECT id FROM income_sources WHERE household_id = ?)`
const ownedScenario = ` AND scenario_id     IN (SELECT id FROM scenarios      WHERE household_id = ?)`
const ownedTrip     = ` AND trip_plan_id    IN (SELECT id FROM trip_plans     WHERE household_id = ?)`
const ownedAccount  = ` AND account_id      IN (SELECT id FROM accounts       WHERE household_id = ?)`
```

Inserts use `INSERT ... SELECT ... WHERE EXISTS (parent owned by household)` and
return `ErrNotFound` when nothing was written.

**The predicate lives in the SQL, not in the handler.** This is deliberate. Four
cross-tenant bugs existed precisely because the guard sat in the caller and the
next caller forgot it. A predicate in the store cannot be forgotten.

### The rule for new code

> Any store method that takes an id **must** also take a `householdID` and use
> it in the statement. If the table has no `household_id`, scope it through its
> parent with the matching `owned*` constant.

A `WHERE id = ?` with no tenant predicate is a cross-tenant read or write. That
is the whole bug class; there is no subtler version of it.

Exempt, legitimately: `UserByEmail` and `UpdateUserPassword` (authentication,
pre-session), `Household(id)` (resolving the tenant itself), the `sessions`
table, and the global `fx_rates` / `gold_prices` caches, which are market data
owned by nobody.

### Import is a trust boundary

`store.ImportHousehold` accepts a user-supplied file. Ids inside it are
untrusted: nothing from the file is used as a primary or foreign key. Rows are
inserted fresh into the *importing* household and every child foreign key is
remapped through an old-id → new-id map. A file exported by one household and
imported by another produces a private copy and cannot touch the original —
asserted in `handler/backup_test.go` by re-exporting the source afterwards and
comparing it byte for byte.

## What is shared between households

- **FX rates** (`internal/fx`) and the **gold spot price** (`internal/gold`) —
  global market data, refetched on a schedule, correctly shared.
- **The manual gold price override** is *not* shared. It used to be:
  `manualOverride()` read `HouseholdIDs()[0]` and applied the lowest-id
  household's price to everyone. `PricePerGramCents` now takes a `householdID`;
  passing `0` means "no household in scope" and falls back to the cached price
  rather than borrowing a stranger's override.

There is no other package-level mutable state keyed by anything but the
household.

## Tests that hold the line

`internal/handler/tenancy_test.go` builds two real households through signup and
asserts, for each child table, that household A cannot read, modify or delete
household B's rows — **and that B's rows are unchanged afterwards**. It also
exercises the legitimate same-household path, so the guarantee is not merely
"deny everything".

When adding a table, add it there. The test that matters is the one asserting
the *victim's* data survives, not just that the attacker got an error.

## Not done: hosting prerequisites

None of these are code bugs. They are the operational work that has not been
done because this instance currently serves one household.

### 1. SQLite is capped at one connection

`store.Open` sets `SetMaxOpenConns(1)`. WAL and a 5s busy timeout are on, but
every query — reads included, across every household — funnels through a single
connection. Fine for one household; a hard throughput ceiling for many.

*To do:* raise the read concurrency (SQLite handles concurrent readers under WAL
fine; the single-writer constraint is the real one), or move to a database built
for it. Measure before choosing.

### 2. No login rate limiting

`POST /login` has no throttle, no lockout, no backoff. bcrypt at cost 10 makes
each guess expensive, but nothing limits the number of guesses.

*To do:* rate-limit at the reverse proxy, or in-app per-IP and per-account. This
is the single most important item on this list.

### 3. Signup is open

An empty invite field creates a new household, so anyone who can reach `/signup`
can create a tenant. Combined with (2), that is unthrottled tenant creation.

*To do:* decide the policy — invite-only instance, an admin allowlist, or open
with rate limiting and abuse monitoring. There is no gate today.

### 4. `SECURE_COOKIES` must be set behind TLS

The session cookie only gets the `Secure` flag when `SECURE_COOKIES=1`. Behind a
TLS-terminating proxy with it unset, session cookies travel without it.

*To do:* it is already in `docker-compose.yml` — set it to `1` in any deployment
reachable over HTTPS. See [self-hosting.md](self-hosting.md).

### 5. No per-household resource limits

Nothing caps how many expenses, scenarios, snapshots or card transactions a
household can create, and the import endpoint accepts up to 8 MiB per request.
One household can degrade the instance for the rest, which matters much more
once (1) is still true.

*To do:* per-household row caps or quotas, and a per-account import rate limit.

### 6. No operational visibility

No per-household metrics, no audit log of logins or destructive actions (import
replaces a household's entire dataset and leaves no trace beyond the result
message), and backups are whatever the operator does with the SQLite file.

*To do:* at minimum, log authentication events and imports with the household
id, and document a backup procedure. Users can already export their own data
from Settings.
