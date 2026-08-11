package main

import (
	"errors"
	"log"

	"github.com/mattmezza/paripari/internal/auth"
	"github.com/mattmezza/paripari/internal/model"
	"github.com/mattmezza/paripari/internal/store"
)

// seed inserts a demo household. It refuses to touch a database that already
// has users, so `make seed` is safe to re-run.
func seed(st *store.Store) error {
	ids, err := st.HouseholdIDs()
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return errors.New("database is not empty — refusing to seed")
	}

	elena, err := auth.Signup(st, "Elena", "elena@example.com", "paripari123", "")
	if err != nil {
		return err
	}
	hh, err := st.Household(elena.HouseholdID)
	if err != nil {
		return err
	}
	marco, err := auth.Signup(st, "Marco", "marco@example.com", "paripari123", hh.InviteCode)
	if err != nil {
		return err
	}

	incomes := []model.IncomeSource{
		{HouseholdID: hh.ID, UserID: elena.ID, Name: "Salary", Kind: "fixed", PayStructure: 13, GrossYearlyCents: 14_000_000, Currency: "CHF"},
		{HouseholdID: hh.ID, UserID: marco.ID, Name: "Salary", Kind: "fixed", PayStructure: 12, GrossYearlyCents: 11_000_000, Currency: "CHF"},
		{HouseholdID: hh.ID, UserID: marco.ID, Name: "Freelance", Kind: "variable", PayStructure: 12, GrossYearlyCents: 1_200_000, Currency: "EUR"},
	}
	for i := range incomes {
		id, err := st.CreateIncome(&incomes[i])
		if err != nil {
			return err
		}
		if _, err := st.CreateDeduction(hh.ID, &model.IncomeDeduction{
			IncomeSourceID: id, Name: "Tax at source", AmountCents: incomes[i].GrossYearlyCents / 5, Period: "yearly",
		}); err != nil {
			return err
		}
		if _, err := st.CreateDeduction(hh.ID, &model.IncomeDeduction{
			IncomeSourceID: id, Name: "Social insurance", Period: "percent", PercentBP: 530,
		}); err != nil {
			return err
		}
	}

	expenses := []model.Expense{
		{HouseholdID: hh.ID, Name: "Rent", AmountCents: 240_000, Currency: "CHF", Category: "common", Subcategory: "housing"},
		{HouseholdID: hh.ID, Name: "Utilities", AmountCents: 22_000, Currency: "CHF", Category: "common", Subcategory: "housing"},
		{HouseholdID: hh.ID, Name: "Health insurance", AmountCents: 68_000, Currency: "CHF", Category: "common", Subcategory: "insurance"},
		{HouseholdID: hh.ID, Name: "Groceries", AmountCents: 90_000, Currency: "CHF", Category: "common", Subcategory: "food"},
		{HouseholdID: hh.ID, Name: "Common savings", AmountCents: 150_000, Currency: "CHF", Category: "common", Subcategory: "savings", IsSavings: true},
		{HouseholdID: hh.ID, Name: "Gym", AmountCents: 9_000, Currency: "CHF", Category: "personal", UserID: &elena.ID, Subcategory: "health"},
		{HouseholdID: hh.ID, Name: "Motorbike", AmountCents: 18_000, Currency: "CHF", Category: "personal", UserID: &marco.ID, Subcategory: "transport"},
	}
	for i := range expenses {
		if _, err := st.CreateExpense(&expenses[i]); err != nil {
			return err
		}
	}

	accounts := []model.Account{
		{HouseholdID: hh.ID, Name: "Common checking", Institution: "Migros Bank", Currency: "CHF", BalanceCents: 850_000, Purpose: "checking"},
		{HouseholdID: hh.ID, Name: "Common savings", Institution: "Migros Bank", Currency: "CHF", BalanceCents: 4_500_000, Purpose: "savings"},
		{HouseholdID: hh.ID, Name: "CC buffer", Institution: "Migros Bank", Currency: "CHF", BalanceCents: 120_000, Purpose: "cc_buffer"},
		{HouseholdID: hh.ID, Name: "Holiday budget", Institution: "Migros Bank", Currency: "CHF", BalanceCents: 300_000, Purpose: "envelope"},
		{HouseholdID: hh.ID, UserID: &elena.ID, Name: "Elena savings", Institution: "PostFinance", Currency: "CHF", BalanceCents: 2_100_000, Purpose: "savings"},
		{HouseholdID: hh.ID, UserID: &marco.ID, Name: "Marco savings", Institution: "Garanti", Currency: "EUR", BalanceCents: 1_400_000, Purpose: "savings"},
		{HouseholdID: hh.ID, Name: "Investments", Institution: "IBKR", Currency: "USD", BalanceCents: 6_000_000, Purpose: "investment"},
	}
	for i := range accounts {
		if _, err := st.CreateAccount(&accounts[i]); err != nil {
			return err
		}
	}

	if _, err := st.CreateGoldItem(&model.GoldItem{HouseholdID: hh.ID, Name: "Coins", WeightGrams: 31.1, PurityKarat: 24, Quantity: 4, Location: "bank safe"}); err != nil {
		return err
	}
	if _, err := st.CreateAsset(&model.Asset{HouseholdID: hh.ID, Name: "Apartment", Kind: "real_estate", EstimatedValueCents: 78_000_000, Currency: "EUR"}); err != nil {
		return err
	}
	if _, err := st.CreateGoal(&model.Goal{HouseholdID: hh.ID, Name: "Safety net", TargetAmountCents: 6_000_000, Currency: "CHF", Category: "safety"}); err != nil {
		return err
	}
	if _, err := st.CreateGoal(&model.Goal{HouseholdID: hh.ID, Name: "House deposit", TargetAmountCents: 25_000_000, Currency: "CHF", Category: "property"}); err != nil {
		return err
	}

	tripID, err := st.CreateTrip(&model.TripPlan{HouseholdID: hh.ID, Name: "Japan, spring", MonthsToSave: 10})
	if err != nil {
		return err
	}
	for _, it := range []model.TripItem{
		{TripPlanID: tripID, Name: "Flights", Category: "transport", AmountCents: 180_000, Currency: "CHF"},
		{TripPlanID: tripID, Name: "Hotels", Category: "accommodation", AmountCents: 220_000, Currency: "CHF"},
		{TripPlanID: tripID, Name: "Food & activities", Category: "living", AmountCents: 150_000, Currency: "CHF"},
	} {
		if _, err := st.CreateTripItem(hh.ID, &it); err != nil {
			return err
		}
	}

	log.Printf("seeded household %q — log in as elena@example.com / paripari123 (invite code %s)", hh.Name, hh.InviteCode)
	return nil
}
