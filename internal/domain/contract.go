package domain

import (
	"fmt"
	"strings"
	"time"

	"azper/internal/identity"
)

type Contract struct {
	ID                string    `json:"id"`
	Objective         string    `json:"objective"`
	SuccessConditions []string  `json:"success_conditions"`
	CreatedAt         time.Time `json:"created_at"`
}

func NewContract(objective string, successConditions []string, now time.Time) (Contract, error) {
	contract := Contract{
		Objective:         strings.TrimSpace(objective),
		SuccessConditions: cleanStrings(successConditions),
		CreatedAt:         now.UTC(),
	}

	id, err := identity.New("ctr", now)
	if err != nil {
		return Contract{}, fmt.Errorf("create contract identifier: %w", err)
	}
	contract.ID = id
	if err := contract.Validate(); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

func (c Contract) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("contract id is required")
	}
	if strings.TrimSpace(c.Objective) == "" {
		return fmt.Errorf("contract objective is required")
	}
	if len(c.SuccessConditions) == 0 {
		return fmt.Errorf("at least one success condition is required")
	}
	for i, condition := range c.SuccessConditions {
		if strings.TrimSpace(condition) == "" {
			return fmt.Errorf("success condition %d is empty", i+1)
		}
	}
	if c.CreatedAt.IsZero() {
		return fmt.Errorf("contract creation time is required")
	}
	return nil
}

func cleanStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}
