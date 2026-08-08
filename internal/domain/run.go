package domain

import (
	"fmt"
	"strings"
	"time"

	"azper/internal/identity"
)

type RunStatus string

const (
	RunRunning            RunStatus = "Running"
	RunCompleted          RunStatus = "Completed"
	RunPartiallyCompleted RunStatus = "PartiallyCompleted"
	RunBlocked            RunStatus = "Blocked"
	RunFailed             RunStatus = "Failed"
	RunCancelled          RunStatus = "Cancelled"
	RunSuperseded         RunStatus = "Superseded"
)

type Run struct {
	ID         string    `json:"id"`
	ContractID string    `json:"contract_id"`
	Status     RunStatus `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func NewRun(contractID string, now time.Time) (Run, error) {
	run := Run{
		ContractID: strings.TrimSpace(contractID),
		Status:     RunRunning,
		CreatedAt:  now.UTC(),
		UpdatedAt:  now.UTC(),
	}
	if run.ContractID == "" {
		return Run{}, fmt.Errorf("contract id is required")
	}

	id, err := identity.New("run", now)
	if err != nil {
		return Run{}, fmt.Errorf("create run identifier: %w", err)
	}
	run.ID = id
	return run, nil
}

func (r Run) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(r.ContractID) == "" {
		return fmt.Errorf("run contract id is required")
	}
	if r.Status != RunRunning {
		return fmt.Errorf("new run must be in %s state", RunRunning)
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return fmt.Errorf("run timestamps are required")
	}
	return nil
}
