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
	PricePerGramCents(currency string) (int64, error)
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
		pricePerGramCHF, _ = s.gold.PricePerGramCents("CHF")
	}

	nw := service.ComputeNetWorth(service.Inputs{
		Accounts:              accounts,
		Assets:                realEstateOnly,
		Gold:                  items,
		Rates:                 s.rates,
		Display:               "CHF",
		GoldPricePerGramCents: pricePerGramCHF,
	})

	return s.db.PutSnapshot(&model.NetWorthSnapshot{
		HouseholdID:      householdID,
		LiquidCents:      nw.LiquidCents,
		AlternativeCents: nw.AlternativeCents,
		RealEstateCents:  nw.RealEstateCents,
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
