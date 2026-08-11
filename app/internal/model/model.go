// Package model holds the domain structs mirroring the SQLite schema.
// Money is always INTEGER minor units (cents). Timestamps are ISO8601 UTC strings.
package model

type Household struct {
	ID                    int64
	Name                  string
	SplitMethod           string // fifty_fifty | income_weighted
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
}

type Session struct {
	Token     string
	UserID    int64
	CreatedAt string
	ExpiresAt string
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
	IsSavings   bool
	// AccountID is the budget-holder account this expense is accumulated in.
	// nil means the household default (common savings / common checking).
	AccountID *int64
	CreatedAt string
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
	CreatedAt    string
	Items        []TripItem
}

type TripItem struct {
	ID          int64
	TripPlanID  int64
	Name        string
	Category    string
	AmountCents int64
	Currency    string
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
