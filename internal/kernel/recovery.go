package kernel

import (
	"context"

	"azper/internal/domain"
	"azper/internal/fault"
)

type RecoveryOutcome struct {
	EffectID           string                    `json:"effect_id,omitempty"`
	CompensationID     string                    `json:"compensation_id,omitempty"`
	Status             domain.EffectStatus       `json:"status,omitempty"`
	CompensationStatus domain.CompensationStatus `json:"compensation_status,omitempty"`
	Category           fault.Category            `json:"category,omitempty"`
	Error              string                    `json:"error,omitempty"`
}

type RecoveryReport struct {
	Inspected      int               `json:"inspected"`
	Committed      int               `json:"committed"`
	Compensated    int               `json:"compensated"`
	NeedsAttention int               `json:"needs_attention"`
	Outcomes       []RecoveryOutcome `json:"outcomes"`
}

type RecoveryStore interface {
	EffectStore
	CompensationStore
	CompensationsByStatus(context.Context, domain.CompensationStatus) ([]domain.Compensation, error)
}

type RecoveryEngine struct {
	store                RecoveryStore
	executor             *FileEffectEngine
	verifier             *FileVerifier
	compensationExecutor *FileCompensationEngine
	compensationVerifier *FileCompensationVerifier
}

func NewRecoveryEngine(store RecoveryStore, workerID string) (*RecoveryEngine, error) {
	executor, err := NewFileEffectEngine(store, workerID)
	if err != nil {
		return nil, err
	}
	verifier, err := NewFileVerifier(store)
	if err != nil {
		return nil, err
	}
	compensationExecutor, err := NewFileCompensationEngine(store, workerID)
	if err != nil {
		return nil, err
	}
	compensationVerifier, err := NewFileCompensationVerifier(store)
	if err != nil {
		return nil, err
	}
	return &RecoveryEngine{
		store: store, executor: executor, verifier: verifier,
		compensationExecutor: compensationExecutor, compensationVerifier: compensationVerifier,
	}, nil
}

func (r *RecoveryEngine) Recover(ctx context.Context) (RecoveryReport, error) {
	effects, err := r.store.EffectsByStatus(ctx, domain.EffectExecuting)
	if err != nil {
		return RecoveryReport{}, err
	}
	compensations, err := r.store.CompensationsByStatus(ctx, domain.CompensationExecuting)
	if err != nil {
		return RecoveryReport{}, err
	}
	report := RecoveryReport{
		Inspected: len(effects) + len(compensations),
		Outcomes:  make([]RecoveryOutcome, 0, len(effects)+len(compensations)),
	}
	for _, pending := range effects {
		if err := ctx.Err(); err != nil {
			return report, fault.New("recovery.run", fault.Cancelled, err)
		}
		outcome := RecoveryOutcome{EffectID: pending.ID, Status: pending.Status}
		executed, recoveryErr := r.executor.Execute(ctx, pending.ID)
		if recoveryErr == nil && executed.Status == domain.EffectExecuted {
			executed, _, recoveryErr = r.verifier.Verify(ctx, executed.ID)
		}
		if recoveryErr != nil {
			outcome.Category = fault.CategoryOf(recoveryErr)
			outcome.Error = recoveryErr.Error()
			current, readErr := r.store.Effect(ctx, pending.ID)
			if readErr == nil {
				outcome.Status = current.Status
			}
			report.NeedsAttention++
			if outcome.Category == fault.Cancelled {
				report.Outcomes = append(report.Outcomes, outcome)
				return report, recoveryErr
			}
		} else {
			outcome.Status = executed.Status
			if executed.Status == domain.EffectCommitted {
				report.Committed++
			} else {
				report.NeedsAttention++
			}
		}
		report.Outcomes = append(report.Outcomes, outcome)
	}
	for _, pending := range compensations {
		if err := ctx.Err(); err != nil {
			return report, fault.New("recovery.run", fault.Cancelled, err)
		}
		outcome := RecoveryOutcome{CompensationID: pending.ID, CompensationStatus: pending.Status}
		executed, recoveryErr := r.compensationExecutor.Execute(ctx, pending.ID)
		if recoveryErr == nil && executed.Status == domain.CompensationExecuted {
			executed, _, recoveryErr = r.compensationVerifier.Verify(ctx, executed.ID)
		}
		if recoveryErr != nil {
			outcome.Category = fault.CategoryOf(recoveryErr)
			outcome.Error = recoveryErr.Error()
			current, readErr := r.store.Compensation(ctx, pending.ID)
			if readErr == nil {
				outcome.CompensationStatus = current.Status
			}
			report.NeedsAttention++
			if outcome.Category == fault.Cancelled {
				report.Outcomes = append(report.Outcomes, outcome)
				return report, recoveryErr
			}
		} else {
			outcome.CompensationStatus = executed.Status
			if executed.Status == domain.CompensationCompensated {
				report.Compensated++
			} else {
				report.NeedsAttention++
			}
		}
		report.Outcomes = append(report.Outcomes, outcome)
	}
	return report, nil
}
