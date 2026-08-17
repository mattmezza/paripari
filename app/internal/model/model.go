// Package model holds the domain structs mirroring the SQLite schema.
// Money is always INTEGER minor units (cents). Timestamps are ISO8601 UTC strings.
package model

import "encoding/json"

type Household struct {
	ID                    int64
	Name                  string
	SplitMethod           string // fifty_fifty | income_weighted
	WeightBasis           string // net | gross — which income the weighting follows
	IncludeVariableIncome bool
	DisplayCurrency       string
	ManualGoldPriceCents  *int64
	InviteCode            string
	CreatedAt             string
}

type User struct {
	ID           int64
	HouseholdID  int64
	Email        string
	PasswordHash string
	Name         string
	CreatedAt    string
	// SessionExpiryDays is the session lifetime in days; nil means "never
	// expire" (NULL in the database). 30 is the default for new users.
	SessionExpiryDays *int
	// TOTPSecret is the base32 secret; nil unless the user started enrollment.
	TOTPSecret string
	// TOTPEnabled is true once a valid code activated the secret.
	TOTPEnabled bool
	// RecoveryCodes holds SHA-256 hex digests of the single-use recovery
	// codes. Plaintext codes are shown exactly once, at activation/rotation.
	RecoveryCodes []string
}

type Session struct {
	Token     string
	UserID    int64
	CreatedAt string
	// ExpiresAt is the ISO8601 expiry; "" means the session never expires.
	ExpiresAt string
	// VerifiedAt is the ISO8601 timestamp of the last step-up re-auth; ""
	// means the session has never passed a step-up challenge.
	VerifiedAt string
}

type IncomeSource struct {
	ID               int64
	HouseholdID      int64
	UserID           int64
	Name             string
	Kind             string // fixed | variable
	PayStructure     int    // 12 | 13
	GrossYearlyCents int64
	Currency         string
	CreatedAt        string
	Deductions       []IncomeDeduction
}

type IncomeDeduction struct {
	ID             int64
	IncomeSourceID int64
	Name           string
	AmountCents    int64
	Period         string // monthly | yearly | percent
	// PercentBP is basis points of gross yearly (5.30% = 530), read only when
	// Period is "percent". AmountCents is 0 for those.
	PercentBP int64
}

type Expense struct {
	ID          int64
	HouseholdID int64
	Name        string
	AmountCents int64
	Currency    string
	Category    string // personal | common
	UserID      *int64 // set iff personal
	Subcategory string
	// Kind is the Tag field: what this money actually is. Anything other than
	// KindExpense is money kept rather than spent — see IsSavings.
	Kind string
	// AccountID is the budget-holder account this expense is accumulated in.
	// nil means the household default (common savings / common checking).
	AccountID *int64
	CreatedAt string
}

// Expense kinds. An empty kind reads as KindExpense: rows written before the
// tag existed, and the zero value, are both regular expenses.
const (
	KindExpense    = "expense"
	KindSavings    = "savings"
	KindInvestment = "investment"
	KindPension    = "pension"
)

// ExpenseKinds is the Tag field's option list, in display order.
var ExpenseKinds = []struct{ Value, Label string }{
	{KindExpense, "Regular expense"},
	{KindSavings, "Savings"},
	{KindInvestment, "Investment"},
	{KindPension, "Pension"},
}

// IsSavings reports whether this is money kept rather than spent. Every
// calculation treats investment and pension exactly as it treated savings
// before the tag was widened, so this stays the one question they ask.
func (e Expense) IsSavings() bool { return e.Kind != "" && e.Kind != KindExpense }

// KindForSubcategory resolves the tag actually stored: a row filed under a
// "savings" subcategory but left tagged as a regular expense would silently
// drop out of the household's savings figure, so the subcategory wins.
func KindForSubcategory(kind, subcategory string) string {
	if kind != "" && kind != KindExpense {
		return kind
	}
	if subcategory == KindSavings {
		return KindSavings
	}
	return KindExpense
}

// ValidExpenseKind reports whether k is one of the four tags.
func ValidExpenseKind(k string) bool {
	for _, v := range ExpenseKinds {
		if v.Value == k {
			return true
		}
	}
	return false
}

// UnmarshalJSON accepts backups written before the tag existed, where the
// field was a boolean is_savings.
func (e *Expense) UnmarshalJSON(b []byte) error {
	type alias Expense // no methods, so no recursion
	var v struct {
		alias
		IsSavings *bool `json:"IsSavings"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*e = Expense(v.alias)
	if e.Kind == "" {
		e.Kind = KindExpense
		if v.IsSavings != nil && *v.IsSavings {
			e.Kind = KindSavings
		}
	}
	return nil
}

type Account struct {
	ID           int64
	HouseholdID  int64
	UserID       *int64
	Name         string
	Institution  string
	IBAN         string
	Currency     string
	BalanceCents int64
	Purpose      string // checking | savings | investment | cc_buffer | envelope | pension
	CreatedAt    string
}

type CCTransaction struct {
	ID            int64
	AccountID     int64
	Description   string
	AmountCents   int64
	Currency      string
	CashbackCents int64
	CreatedAt     string
}

type GoldItem struct {
	ID          int64
	HouseholdID int64
	Name        string
	WeightGrams float64
	PurityKarat int
	Quantity    int
	Location    string
	CreatedAt   string
}

type Asset struct {
	ID                  int64
	HouseholdID         int64
	Name                string
	Kind                string // real_estate | other
	EstimatedValueCents int64
	Currency            string
	CreatedAt           string
}

type Goal struct {
	ID                int64
	HouseholdID       int64
	Name              string
	TargetAmountCents int64
	Currency          string
	Deadline          *string
	Category          string
	CreatedAt         string
}

type Scenario struct {
	ID          int64
	HouseholdID int64
	Name        string
	Description string
	CreatedAt   string
	Changes     []ScenarioChange
}

type ScenarioChange struct {
	ID         int64
	ScenarioID int64
	ChangeType string
	TargetID   *int64
	Label      string
	ValueCents *int64
	ValueNum   *float64
	ValueText  string
	Currency   string
}

type TripPlan struct {
	ID           int64
	HouseholdID  int64
	Name         string
	StartDate    *string
	MonthsToSave int
	Committed    bool
	// FundingAccountID is the account the trip money sits in, usually a budget
	// envelope. nil means the household default (common checking).
	FundingAccountID *int64
	// FundingStrategy is TripSpread or TripOneShot; "" reads as TripSpread.
	FundingStrategy string
	// LinkedExpenseID is the recurring expense a commit created, so uncommitting
	// can remove exactly that row. nil for a draft, or for a committed one-shot
	// trip, which creates no expense at all.
	LinkedExpenseID *int64
	CreatedAt       string
	Items           []TripItem
}

// Trip funding strategies. An empty strategy reads as TripSpread: rows written
// before the choice existed put money aside every month, and so does the zero
// value.
const (
	TripSpread  = "spread"
	TripOneShot = "one_shot"
)

// TripStrategies is the funding picker's option list, in display order.
var TripStrategies = []struct{ Value, Label string }{
	{TripSpread, "Save up monthly"},
	{TripOneShot, "Pay in one go"},
}

// IsOneShot reports whether the trip is paid out of what the account already
// holds rather than funded month by month.
func (t TripPlan) IsOneShot() bool { return t.FundingStrategy == TripOneShot }

// ValidTripStrategy reports whether s is one of the two funding strategies.
// The empty string is not valid on the way in — handlers default it explicitly.
func ValidTripStrategy(s string) bool { return s == TripSpread || s == TripOneShot }

type TripItem struct {
	ID          int64
	TripPlanID  int64
	Name        string
	Category    string
	AmountCents int64
	Currency    string
}

// SavedProjection is a named set of projection assumptions. Params is the
// projections page's query string verbatim; loading one is /projections?Params.
type SavedProjection struct {
	ID          int64
	HouseholdID int64
	Name        string
	Params      string
	CreatedAt   string
}

type FXRate struct {
	Base      string
	Quote     string
	Rate      float64
	FetchedAt string
}

type GoldPrice struct {
	ID                int64
	PricePerGramCents int64
	Currency          string
	FetchedAt         string
}

// NetWorthSnapshot amounts are CHF-converted at snapshot time.
type NetWorthSnapshot struct {
	ID               int64
	HouseholdID      int64
	Date             string // YYYY-MM-DD
	LiquidCents      int64
	AlternativeCents int64
	RealEstateCents  int64
}

type TransferConfirmation struct {
	ID          int64
	HouseholdID int64
	Payload     string // JSON snapshot of confirmed transfer amounts
	ConfirmedAt string
}

// FinancialSnapshot is the household's monthly cash-flow picture as it stood
// on Date. Because expenses are a recurring monthly model rather than
// transactions, consecutive rows are flat until something is edited — the
// history is a step function, not a spend log. Amounts are CHF-converted at
// snapshot time.
type FinancialSnapshot struct {
	ID                    int64
	HouseholdID           int64
	Date                  string // YYYY-MM-DD
	Currency              string
	IncomeCents           int64
	ExpensesCents         int64 // non-savings expenses, both partners
	SavingsCents          int64
	AvailableCents        int64 // disposable: income minus everything out
	CommonExpensesCents   int64
	PersonalExpensesCents int64
}
