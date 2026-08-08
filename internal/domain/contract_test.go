package domain

import (
	"testing"
	"time"
)

func TestNewContractNormalizesInput(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.FixedZone("test", -7*60*60))
	contract, err := NewContract("  preserve execution truth  ", []string{" persisted ", "", " event recorded "}, now)
	if err != nil {
		t.Fatal(err)
	}

	if contract.Objective != "preserve execution truth" {
		t.Fatalf("objective = %q", contract.Objective)
	}
	if len(contract.SuccessConditions) != 2 {
		t.Fatalf("success conditions = %#v", contract.SuccessConditions)
	}
	if contract.CreatedAt.Location() != time.UTC {
		t.Fatalf("creation time location = %v", contract.CreatedAt.Location())
	}
}

func TestNewContractRejectsMissingSemantics(t *testing.T) {
	tests := []struct {
		name       string
		objective  string
		conditions []string
	}{
		{name: "objective", conditions: []string{"done"}},
		{name: "success condition", objective: "do work"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewContract(test.objective, test.conditions, time.Now()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
