package model

import (
	"encoding/json"
	"testing"
)

// Backups written before the Tag field was widened carry a boolean is_savings.
// They must still import as savings, or a restore silently turns kept money
// into spending.
func TestExpenseUnmarshalLegacyIsSavings(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"legacy savings", `{"Name":"Fund","IsSavings":true}`, KindSavings},
		{"legacy expense", `{"Name":"Rent","IsSavings":false}`, KindExpense},
		{"no tag at all", `{"Name":"Rent"}`, KindExpense},
		{"current format", `{"Name":"Pillar 3a","Kind":"pension"}`, KindPension},
		{"kind wins over the legacy flag", `{"Kind":"investment","IsSavings":false}`, KindInvestment},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var e Expense
			if err := json.Unmarshal([]byte(tc.in), &e); err != nil {
				t.Fatal(err)
			}
			if e.Kind != tc.want {
				t.Errorf("Kind = %q, want %q", e.Kind, tc.want)
			}
			if got := e.IsSavings(); got != (tc.want != KindExpense) {
				t.Errorf("IsSavings() = %v for kind %q", got, e.Kind)
			}
		})
	}
}
