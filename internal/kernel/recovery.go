package kernel

import (
	"context"

	"azper/internal/domain"
	"azper/internal/fault"
)

type RecoveryOutcome struct {
	EffectID string              `json:"effect_id"`
	Status   domain.EffectStatus `json:"status"`
	Category fault.Category      `json:"category,omitempty"`
	Error    string              `json:"error,omitempty"`
}

type RecoveryReport struct {
	Inspected      int               `json:"inspected"`
	Committed      int               `json:"committed"`
	NeedsAttention int               `json:"needs_attention"`
	Outcomes       []RecoveryOutcome `json:"outcomes"`
}

type RecoveryEngine struct {
	store    EffectStore
	executor *FileEffectEngine
	verifier *FileVerifier
}

func NewRecoveryEngine(store EffectStore, workerID string) (*RecoveryEngine, error) {
	executor, err := NewFileEffectEngine(store, workerID)
	if err != nil {
		return nil, err
	}
	verifier, err := NewFileVerifier(store)
	if err != nil {
		return nil, err
	}
	return &RecoveryEngine{store: store, executor: executor, verifier: verifier}, nil
}

func (r *RecoveryEngine) Recover(ctx context.Context) (RecoveryReport, error) {
	effects, err := r.store.EffectsByStatus(ctx, domain.EffectExecuting)
	if err != nil {
		return RecoveryReport{}, err
	}
	report := RecoveryReport{Inspected: len(effects), Outcomes: make([]RecoveryOutcome, 0, len(effects))}
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
	return report, nil
}
