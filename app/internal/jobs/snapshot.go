// Package jobs holds background DailyJob implementations that don't belong
// to a single external-data source (net worth snapshotting spans accounts,
// gold and assets).
package jobs

import (
	"context"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/service"
	"github.com/mattmezza/paripari/internal/store"
)

// RateConverter matches view.RateProvider's shape.
type RateConverter interface {
	Convert(amountCents int64, from, to string) int64
}

// GoldPriceProvider matches handler.GoldProvider's shape.
type GoldPriceProvider interface {
	PricePerGramCents(householdID int64, currency string) (int64, error)
}

// Snapshotter is the net-worth-snapshot DailyJob. It also exposes
// SnapshotHousehold so handlers can snapshot on edit. The bucket math itself
// lives in internal/service.ComputeNetWorth; this just gathers the inputs and
// persists the result.
type Snapshotter struct {
	db    *store.Store
	rates RateConverter
	gold  GoldPriceProvider
}

func NewSnapshotter(db *store.Store, rates RateConverter, gold GoldPriceProvider) *Snapshotter {
	return &Snapshotter{db: db, rates: rates, gold: gold}
}

func (s *Snapshotter) Name() string { return "net-worth-snapshot" }

func (s *Snapshotter) Run(ctx context.Context) error {
	ids, err := s.db.HouseholdIDs()
	if err != nil {
		return err
	}
	var firstErr error
	for _, id := range ids {
		if err := s.SnapshotHousehold(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SnapshotHousehold computes and upserts today's net worth snapshot (in CHF)
// for one household. Exported so handlers can call it directly after an
// account/gold/asset edit (snapshot-on-edit).
func (s *Snapshotter) SnapshotHousehold(ctx context.Context, householdID int64) error {
	accounts, err := s.db.Accounts(householdID)
	if err != nil {
		return err
	}
	items, err := s.db.GoldItems(householdID)
	if err != nil {
		return err
	}
	assets, err := s.db.Assets(householdID)
	if err != nil {
		return err
	}
	// real_estate is its own net-worth bucket; other asset kinds aren't
	// counted here.
	realEstateOnly := assets[:0:0]
	for _, a := range assets {
		if a.Kind == "real_estate" {
			realEstateOnly = append(realEstateOnly, a)
		}
	}

	var pricePerGramCHF int64
	if len(items) > 0 {
		// no cached/manual gold price yet: value gold as 0, don't fail the snapshot
		pricePerGramCHF, _ = s.gold.PricePerGramCents(householdID, "CHF")
	}

	nw := service.ComputeNetWorth(service.Inputs{
		Accounts:              accounts,
		Assets:                realEstateOnly,
		Gold:                  items,
		Rates:                 s.rates,
		Display:               "CHF",
		GoldPricePerGramCents: pricePerGramCHF,
	})

	if err := s.db.PutSnapshot(&model.NetWorthSnapshot{
		HouseholdID:      householdID,
		LiquidCents:      nw.LiquidCents,
		AlternativeCents: nw.AlternativeCents,
		RealEstateCents:  nw.RealEstateCents,
	}); err != nil {
		return err
	}
	return s.snapshotFinancial(householdID)
}

// snapshotFinancial upserts today's monthly cash-flow picture (in CHF).
// Expenses are a recurring monthly model, so this records "what the monthly
// picture looked like today" rather than actual spend — flat between edits.
func (s *Snapshotter) snapshotFinancial(householdID int64) error {
	hh, err := s.db.Household(householdID)
	if err != nil {
		return err
	}
	users, err := s.db.UsersByHousehold(householdID)
	if err != nil {
		return err
	}
	incomes, err := s.db.Incomes(householdID)
	if err != nil {
		return err
	}
	expenses, err := s.db.Expenses(householdID)
	if err != nil {
		return err
	}

	in := service.Inputs{
		Household: *hh,
		Incomes:   incomes,
		Expenses:  expenses,
		Rates:     s.rates,
		Display:   "CHF",
	}
	// UsersByHousehold orders by id, which is the PartnerA/PartnerB convention.
	if len(users) > 0 {
		in.PartnerA = users[0]
	}
	if len(users) > 1 {
		in.PartnerB = users[1]
	}
	ov := service.BuildOverview(in)

	return s.db.PutFinancialSnapshot(&model.FinancialSnapshot{
		HouseholdID:           householdID,
		Currency:              "CHF",
		IncomeCents:           ov.TotalIncomeCents,
		ExpensesCents:         ov.CommonExpensesCents + ov.PersonalExpensesCents,
		SavingsCents:          ov.TotalSavingsCents,
		AvailableCents:        ov.AvailableCents,
		CommonExpensesCents:   ov.CommonExpensesCents,
		PersonalExpensesCents: ov.PersonalExpensesCents,
	})
}

// Default is set by main.go once the Snapshotter is constructed, so handlers
// in other packages can trigger a snapshot-on-edit without threading the
// dependency through Deps.
var Default *Snapshotter

// SnapshotHousehold snapshots via Default, if wired. Safe to call before
// startup wiring completes (no-op).
func SnapshotHousehold(ctx context.Context, householdID int64) error {
	if Default == nil {
		return nil
	}
	return Default.SnapshotHousehold(ctx, householdID)
}
