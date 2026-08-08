package kernel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"azper/internal/artifact"
	"azper/internal/domain"
	"azper/internal/fault"
)

type CompensationStore interface {
	Effect(context.Context, string) (domain.Effect, error)
	CapabilityGrant(context.Context, string) (domain.CapabilityGrant, error)
	Artifact(context.Context, string) (domain.Artifact, error)
	StageCompensation(context.Context, domain.Compensation) (domain.Compensation, bool, error)
	Compensation(context.Context, string) (domain.Compensation, error)
	BeginCompensationExecution(context.Context, string, time.Time) (domain.Compensation, error)
	CompleteCompensationExecution(context.Context, string, domain.Artifact, domain.CompensationEvidence, time.Time) (domain.Compensation, error)
	MarkCompensationAmbiguous(context.Context, string, string, time.Time) error
	RecordCompensationVerification(context.Context, string, domain.Artifact, domain.CompensationEvidence, domain.CompensationVerification, time.Time) (domain.Compensation, error)
	CompensationVerifications(context.Context, string) ([]domain.CompensationVerification, error)
}

type FileCompensationEngine struct {
	store        CompensationStore
	workerID     string
	now          func() time.Time
	maxFileBytes int64
}

func NewFileCompensationEngine(store CompensationStore, workerID string) (*FileCompensationEngine, error) {
	if store == nil || strings.TrimSpace(workerID) == "" {
		return nil, fmt.Errorf("compensation store and worker id are required")
	}
	return &FileCompensationEngine{
		store: store, workerID: strings.TrimSpace(workerID), now: time.Now,
		maxFileBytes: DefaultMaxFileBytes,
	}, nil
}

func (e *FileCompensationEngine) Stage(ctx context.Context, effectID, capabilityGrantID string) (domain.Compensation, bool, error) {
	const op = "file_compensation.stage"
	effect, err := e.store.Effect(ctx, effectID)
	if err != nil {
		return domain.Compensation{}, false, err
	}
	if effect.Status != domain.EffectCommitted {
		return domain.Compensation{}, false, fault.New(op, fault.Conflict, fmt.Errorf("effect %q is %s, not Committed", effect.ID, effect.Status))
	}
	grant, err := e.store.CapabilityGrant(ctx, capabilityGrantID)
	if err != nil {
		return domain.Compensation{}, false, err
	}
	now := e.now().UTC()
	if err := authorizeCompensation(grant, effect, e.workerID, now); err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Invalid, err)
	}
	target, err := resolveScopedTarget(grant.Scope, effect.Target)
	if err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Invalid, err)
	}
	if target != effect.Target {
		return domain.Compensation{}, false, fault.New(op, fault.Invalid, errors.New("effect target no longer resolves to its staged path"))
	}
	compensation, err := domain.NewCompensation(effect, grant.ID, now)
	if err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Invalid, err)
	}
	staged, created, err := e.store.StageCompensation(ctx, compensation)
	if err != nil {
		return domain.Compensation{}, false, err
	}
	if staged.EffectID != compensation.EffectID || staged.Target != compensation.Target || staged.RestoreArtifactID != compensation.RestoreArtifactID || staged.RemoveTarget != compensation.RemoveTarget {
		return domain.Compensation{}, false, fault.New(op, fault.Conflict, errors.New("existing compensation does not match the staged previous state"))
	}
	return staged, created, nil
}

func (e *FileCompensationEngine) Execute(ctx context.Context, compensationID string) (domain.Compensation, error) {
	const op = "file_compensation.execute"
	compensation, err := e.store.Compensation(ctx, compensationID)
	if err != nil {
		return domain.Compensation{}, err
	}
	if compensation.Status == domain.CompensationExecuted || compensation.Status == domain.CompensationCompensated {
		return compensation, nil
	}
	if compensation.Status != domain.CompensationStaged && compensation.Status != domain.CompensationExecuting {
		return domain.Compensation{}, fault.New(op, fault.Conflict, fmt.Errorf("compensation %q cannot execute from %s", compensation.ID, compensation.Status))
	}
	effect, err := e.store.Effect(ctx, compensation.EffectID)
	if err != nil {
		return domain.Compensation{}, err
	}
	if effect.Status != domain.EffectCommitted {
		return domain.Compensation{}, fault.New(op, fault.Conflict, fmt.Errorf("effect %q is no longer Committed", effect.ID))
	}
	grant, err := e.store.CapabilityGrant(ctx, compensation.CapabilityGrantID)
	if err != nil {
		return domain.Compensation{}, err
	}
	now := e.now().UTC()
	if err := authorizeCompensationStatic(grant, effect, e.workerID); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Invalid, err)
	}
	target, err := resolveScopedTarget(grant.Scope, compensation.Target)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Invalid, fmt.Errorf("compensation target is outside its recorded scope: %w", err))
	}
	if target != compensation.Target || target != effect.Target {
		return domain.Compensation{}, fault.New(op, fault.Invalid, errors.New("compensation target no longer resolves to the staged effect path"))
	}
	if compensation.Status == domain.CompensationStaged && !grant.ActiveAt(now) {
		return domain.Compensation{}, fault.New(op, fault.Invalid, errors.New("capability grant expired before compensation began"))
	}
	compensation, err = e.store.BeginCompensationExecution(ctx, compensation.ID, now)
	if err != nil {
		return domain.Compensation{}, err
	}
	if compensation.Status == domain.CompensationExecuted || compensation.Status == domain.CompensationCompensated {
		return compensation, nil
	}

	desired, err := e.compensatedArtifact(ctx, compensation)
	if err != nil {
		return domain.Compensation{}, err
	}
	original, err := e.store.Artifact(ctx, effect.DesiredArtifactID)
	if err != nil {
		return domain.Compensation{}, err
	}
	observed, exists, err := observeFile(compensation.Target, e.maxFileBytes)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("reconcile target: %w", err))
	}
	if compensationMatchesObserved(compensation, desired, observed, exists) {
		return e.completeExecution(ctx, compensation, desired)
	}
	if !exists || observed == nil || observed.ID != original.ID {
		return domain.Compensation{}, e.markAmbiguous(ctx, compensation.ID, "target no longer matches the committed effect output")
	}
	if !grant.ActiveAt(now) {
		return domain.Compensation{}, fault.New(op, fault.Invalid, errors.New("capability grant expired before a reconciled retry could compensate state"))
	}

	var mutateErr error
	if compensation.RemoveTarget {
		mutateErr = atomicRemove(ctx, compensation.Target)
	} else {
		mutateErr = atomicWrite(ctx, compensation.Target, desired.Data)
	}
	observed, exists, observeErr := observeFile(compensation.Target, e.maxFileBytes)
	if observeErr == nil && compensationMatchesObserved(compensation, desired, observed, exists) {
		return e.completeExecution(ctx, compensation, desired)
	}
	if observeErr != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("observe target after compensation: %w", observeErr))
	}
	if exists && observed != nil && observed.ID == original.ID && mutateErr != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("compensate target without mutation: %w", mutateErr))
	}
	return domain.Compensation{}, e.markAmbiguous(ctx, compensation.ID, "file compensation outcome could not be reconciled")
}

func (e *FileCompensationEngine) compensatedArtifact(ctx context.Context, compensation domain.Compensation) (domain.Artifact, error) {
	if !compensation.RemoveTarget {
		return e.store.Artifact(ctx, compensation.RestoreArtifactID)
	}
	absent, err := absentArtifact()
	if err != nil {
		return domain.Artifact{}, fault.New("file_compensation.artifact", fault.Internal, err)
	}
	return absent, nil
}

func (e *FileCompensationEngine) completeExecution(ctx context.Context, compensation domain.Compensation, observed domain.Artifact) (domain.Compensation, error) {
	now := e.now().UTC()
	kind := "FileRestoreReadback"
	if compensation.RemoveTarget {
		kind = "FileRemovalReadback"
	}
	evidence, err := domain.NewCompensationEvidence(compensation.ID, observed.ID, kind, compensation.Target, now)
	if err != nil {
		return domain.Compensation{}, fault.New("file_compensation.complete", fault.Internal, err)
	}
	return e.store.CompleteCompensationExecution(ctx, compensation.ID, observed, evidence, now)
}

func (e *FileCompensationEngine) markAmbiguous(ctx context.Context, compensationID, reason string) error {
	if err := e.store.MarkCompensationAmbiguous(ctx, compensationID, reason, e.now().UTC()); err != nil {
		return err
	}
	return fault.New("file_compensation.execute", fault.Ambiguous, errors.New(reason))
}

type FileCompensationVerifier struct {
	store        CompensationStore
	now          func() time.Time
	maxFileBytes int64
}

func NewFileCompensationVerifier(store CompensationStore) (*FileCompensationVerifier, error) {
	if store == nil {
		return nil, fmt.Errorf("compensation verification store is required")
	}
	return &FileCompensationVerifier{store: store, now: time.Now, maxFileBytes: DefaultMaxFileBytes}, nil
}

func (v *FileCompensationVerifier) Verify(ctx context.Context, compensationID string) (domain.Compensation, domain.CompensationVerification, error) {
	const op = "file_compensation_verifier.verify"
	compensation, err := v.store.Compensation(ctx, compensationID)
	if err != nil {
		return domain.Compensation{}, domain.CompensationVerification{}, err
	}
	if compensation.Status == domain.CompensationCompensated {
		verifications, err := v.store.CompensationVerifications(ctx, compensation.ID)
		if err != nil {
			return domain.Compensation{}, domain.CompensationVerification{}, err
		}
		if len(verifications) == 0 {
			return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Internal, errors.New("compensated record has no verification"))
		}
		grant, err := v.store.CapabilityGrant(ctx, compensation.CapabilityGrantID)
		if err != nil {
			return domain.Compensation{}, domain.CompensationVerification{}, err
		}
		target, err := resolveScopedTarget(grant.Scope, compensation.Target)
		if err != nil || target != compensation.Target {
			return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Conflict, errors.New("compensated target no longer resolves within its recorded scope"))
		}
		desired, err := v.compensatedArtifact(ctx, compensation)
		if err != nil {
			return domain.Compensation{}, domain.CompensationVerification{}, err
		}
		observed, exists, err := observeFile(compensation.Target, v.maxFileBytes)
		if err != nil {
			return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Internal, fmt.Errorf("recheck compensated target: %w", err))
		}
		if !compensationMatchesObserved(compensation, desired, observed, exists) {
			return compensation, verifications[len(verifications)-1], fault.New(op, fault.Conflict, errors.New("compensated target has drifted since verification"))
		}
		return compensation, verifications[len(verifications)-1], nil
	}
	if compensation.Status != domain.CompensationExecuted {
		return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Conflict, fmt.Errorf("compensation %q cannot be verified from %s", compensation.ID, compensation.Status))
	}
	grant, err := v.store.CapabilityGrant(ctx, compensation.CapabilityGrantID)
	if err != nil {
		return domain.Compensation{}, domain.CompensationVerification{}, err
	}
	target, err := resolveScopedTarget(grant.Scope, compensation.Target)
	if err != nil || target != compensation.Target {
		return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Invalid, errors.New("compensation target is outside its recorded scope"))
	}

	now := v.now().UTC()
	observed, exists, readErr := observeFile(compensation.Target, v.maxFileBytes)
	if readErr != nil {
		return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Internal, fmt.Errorf("independently read target: %w", readErr))
	}
	desired, err := v.compensatedArtifact(ctx, compensation)
	if err != nil {
		return domain.Compensation{}, domain.CompensationVerification{}, err
	}
	status := domain.VerificationFailed
	kind := "FileHash"
	if compensationMatchesObserved(compensation, desired, observed, exists) {
		status = domain.VerificationPassed
	}
	if !exists {
		kind = "FileAbsent"
		absent, err := absentArtifact()
		if err != nil {
			return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Internal, err)
		}
		observed = &absent
	}
	if observed == nil {
		return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Internal, errors.New("file observation was empty"))
	}
	evidence, err := domain.NewCompensationEvidence(compensation.ID, observed.ID, kind, compensation.Target, now)
	if err != nil {
		return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Internal, err)
	}
	verification, err := domain.NewCompensationVerification(compensation.ID, evidence.ID, "independent BLAKE3-256 filesystem read", status, now)
	if err != nil {
		return domain.Compensation{}, domain.CompensationVerification{}, fault.New(op, fault.Internal, err)
	}
	verified, err := v.store.RecordCompensationVerification(ctx, compensation.ID, *observed, evidence, verification, now)
	if err != nil {
		return domain.Compensation{}, domain.CompensationVerification{}, err
	}
	if status == domain.VerificationFailed {
		return verified, verification, fault.New(op, fault.Conflict, errors.New("target did not satisfy the compensated state"))
	}
	return verified, verification, nil
}

func (v *FileCompensationVerifier) compensatedArtifact(ctx context.Context, compensation domain.Compensation) (domain.Artifact, error) {
	if compensation.RemoveTarget {
		return absentArtifact()
	}
	return v.store.Artifact(ctx, compensation.RestoreArtifactID)
}

func authorizeCompensation(grant domain.CapabilityGrant, effect domain.Effect, workerID string, now time.Time) error {
	if err := authorizeCompensationStatic(grant, effect, workerID); err != nil {
		return err
	}
	if !grant.ActiveAt(now) {
		return errors.New("capability grant is not active")
	}
	return nil
}

func authorizeCompensationStatic(grant domain.CapabilityGrant, effect domain.Effect, workerID string) error {
	if grant.RunID != effect.RunID || grant.WorkerID != workerID || grant.Capability != domain.FilesystemWriteCapability || grant.EffectClass != domain.EffectReversibleWrite {
		return errors.New("capability grant does not authorize compensation of this effect and worker")
	}
	return nil
}

func compensationMatchesObserved(compensation domain.Compensation, desired domain.Artifact, observed *domain.Artifact, exists bool) bool {
	if compensation.RemoveTarget {
		return !exists
	}
	return exists && observed != nil && observed.ID == desired.ID
}

func absentArtifact() (domain.Artifact, error) {
	return artifact.FromBytes([]byte(`{"exists":false}`), "application/vnd.azper.file-observation+json")
}

func atomicRemove(ctx context.Context, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("remove target: %w", err)
	}
	if err := syncDirectory(target); err != nil {
		return err
	}
	return nil
}
