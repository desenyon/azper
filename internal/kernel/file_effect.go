package kernel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"azper/internal/artifact"
	"azper/internal/domain"
	"azper/internal/fault"
)

const DefaultMaxFileBytes int64 = 16 << 20

type EffectStore interface {
	Run(context.Context, string) (domain.Run, error)
	Plan(context.Context, string) (domain.Plan, error)
	CapabilityGrant(context.Context, string) (domain.CapabilityGrant, error)
	StageEffect(context.Context, domain.Effect, domain.Artifact, *domain.Artifact) (domain.Effect, bool, error)
	Effect(context.Context, string) (domain.Effect, error)
	EffectByIdempotency(context.Context, string, string) (domain.Effect, error)
	EffectsByStatus(context.Context, domain.EffectStatus) ([]domain.Effect, error)
	Artifact(context.Context, string) (domain.Artifact, error)
	BeginEffectExecution(context.Context, string, time.Time) (domain.Effect, error)
	CompleteEffectExecution(context.Context, string, domain.Artifact, domain.Evidence, time.Time) (domain.Effect, error)
	MarkEffectAmbiguous(context.Context, string, string, time.Time) error
	RecordVerification(context.Context, string, domain.Artifact, domain.Evidence, domain.Verification, time.Time) (domain.Effect, error)
	VerificationsForEffect(context.Context, string) ([]domain.Verification, error)
}

type FileWriteRequest struct {
	RunID             string
	PlanID            string
	StepID            string
	CapabilityGrantID string
	IdempotencyKey    string
	Target            string
	Content           []byte
}

type FileEffectEngine struct {
	store        EffectStore
	workerID     string
	now          func() time.Time
	maxFileBytes int64
}

func NewFileEffectEngine(store EffectStore, workerID string) (*FileEffectEngine, error) {
	if store == nil || strings.TrimSpace(workerID) == "" {
		return nil, fmt.Errorf("effect store and worker id are required")
	}
	return &FileEffectEngine{
		store: store, workerID: strings.TrimSpace(workerID), now: time.Now,
		maxFileBytes: DefaultMaxFileBytes,
	}, nil
}

func (e *FileEffectEngine) Stage(ctx context.Context, request FileWriteRequest) (domain.Effect, bool, error) {
	const op = "file_effect.stage"
	if int64(len(request.Content)) > e.maxFileBytes {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, fmt.Errorf("content exceeds %d byte limit", e.maxFileBytes))
	}
	run, err := e.store.Run(ctx, request.RunID)
	if err != nil {
		return domain.Effect{}, false, err
	}
	if run.Status != domain.RunRunning {
		return domain.Effect{}, false, fault.New(op, fault.Conflict, fmt.Errorf("run %q is %s", run.ID, run.Status))
	}
	plan, err := e.store.Plan(ctx, request.PlanID)
	if err != nil {
		return domain.Effect{}, false, err
	}
	if plan.ContractID != run.ContractID {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, errors.New("plan and run belong to different contracts"))
	}
	step, err := fileWriteStep(plan, request.StepID)
	if err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, err)
	}
	grant, err := e.store.CapabilityGrant(ctx, request.CapabilityGrantID)
	if err != nil {
		return domain.Effect{}, false, err
	}
	now := e.now().UTC()
	if err := e.authorize(grant, run.ID, step, now); err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, err)
	}
	target, err := resolveScopedTarget(grant.Scope, request.Target)
	if err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, err)
	}
	desired, err := artifact.FromBytes(request.Content, "application/octet-stream")
	if err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Internal, err)
	}
	existing, err := e.store.EffectByIdempotency(ctx, run.ID, request.IdempotencyKey)
	if err == nil {
		if !sameIdempotentRequest(existing, request, target, desired.ID) {
			return domain.Effect{}, false, fault.New(op, fault.Conflict, fmt.Errorf("idempotency key %q already names a different effect", request.IdempotencyKey))
		}
		return existing, false, nil
	}
	if !fault.IsCategory(err, fault.NotFound) {
		return domain.Effect{}, false, err
	}
	previous, _, err := observeFile(target, e.maxFileBytes)
	if err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, fmt.Errorf("observe target before staging: %w", err))
	}
	effect, err := domain.NewFileWriteEffect(run.ID, plan.ID, step.ID, grant.ID, request.IdempotencyKey, target, desired, previous, now)
	if err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, err)
	}
	staged, created, err := e.store.StageEffect(ctx, effect, desired, previous)
	if err != nil {
		return domain.Effect{}, false, err
	}
	if !sameStagedRequest(staged, effect) {
		return domain.Effect{}, false, fault.New(op, fault.Conflict, fmt.Errorf("idempotency key %q already names a different effect", request.IdempotencyKey))
	}
	return staged, created, nil
}

func (e *FileEffectEngine) Execute(ctx context.Context, effectID string) (domain.Effect, error) {
	const op = "file_effect.execute"
	effect, err := e.store.Effect(ctx, effectID)
	if err != nil {
		return domain.Effect{}, err
	}
	if effect.Status == domain.EffectExecuted || effect.Status == domain.EffectCommitted {
		return effect, nil
	}
	if effect.Status != domain.EffectStaged && effect.Status != domain.EffectExecuting {
		return domain.Effect{}, fault.New(op, fault.Conflict, fmt.Errorf("effect %q cannot execute from %s", effect.ID, effect.Status))
	}
	grant, err := e.store.CapabilityGrant(ctx, effect.CapabilityGrantID)
	if err != nil {
		return domain.Effect{}, err
	}
	now := e.now().UTC()
	if grant.RunID != effect.RunID || grant.WorkerID != e.workerID || grant.Capability != domain.FilesystemWriteCapability || grant.EffectClass != effect.Class {
		return domain.Effect{}, fault.New(op, fault.Invalid, errors.New("capability grant does not authorize this effect and worker"))
	}
	target, err := resolveScopedTarget(grant.Scope, effect.Target)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Invalid, fmt.Errorf("effect target is no longer within capability scope: %w", err))
	}
	if target != effect.Target {
		return domain.Effect{}, fault.New(op, fault.Invalid, errors.New("effect target no longer resolves to its staged path"))
	}
	if effect.Status == domain.EffectStaged && !grant.ActiveAt(now) {
		return domain.Effect{}, fault.New(op, fault.Invalid, errors.New("capability grant expired before execution began"))
	}

	effect, err = e.store.BeginEffectExecution(ctx, effect.ID, now)
	if err != nil {
		return domain.Effect{}, err
	}
	if effect.Status == domain.EffectExecuted || effect.Status == domain.EffectCommitted {
		return effect, nil
	}
	desired, err := e.store.Artifact(ctx, effect.DesiredArtifactID)
	if err != nil {
		return domain.Effect{}, err
	}
	var previous *domain.Artifact
	if effect.PreviousExisted {
		artifact, err := e.store.Artifact(ctx, effect.PreviousArtifactID)
		if err != nil {
			return domain.Effect{}, err
		}
		previous = &artifact
	}

	observed, exists, err := observeFile(effect.Target, e.maxFileBytes)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("reconcile target: %w", err))
	}
	if exists && observed.ID == desired.ID {
		return e.completeExecution(ctx, effect, *observed)
	}
	if !matchesPrevious(observed, exists, previous, effect.PreviousExisted) {
		reason := "target matches neither staged previous state nor desired state"
		if err := e.store.MarkEffectAmbiguous(ctx, effect.ID, reason, e.now().UTC()); err != nil {
			return domain.Effect{}, err
		}
		return domain.Effect{}, fault.New(op, fault.Ambiguous, errors.New(reason))
	}
	if !grant.ActiveAt(now) {
		return domain.Effect{}, fault.New(op, fault.Invalid, errors.New("capability grant expired before a reconciled retry could mutate state"))
	}

	writeErr := atomicWrite(ctx, effect.Target, desired.Data)
	observed, exists, observeErr := observeFile(effect.Target, e.maxFileBytes)
	if observeErr == nil && exists && observed.ID == desired.ID {
		return e.completeExecution(ctx, effect, *observed)
	}
	if observeErr != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("observe target after write: %w", observeErr))
	}
	if matchesPrevious(observed, exists, previous, effect.PreviousExisted) && writeErr != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("write target without mutation: %w", writeErr))
	}
	reason := "file write outcome could not be reconciled"
	if err := e.store.MarkEffectAmbiguous(ctx, effect.ID, reason, e.now().UTC()); err != nil {
		return domain.Effect{}, err
	}
	return domain.Effect{}, fault.New(op, fault.Ambiguous, errors.New(reason))
}

func (e *FileEffectEngine) completeExecution(ctx context.Context, effect domain.Effect, observed domain.Artifact) (domain.Effect, error) {
	now := e.now().UTC()
	evidence, err := domain.NewEvidence(effect.ID, observed.ID, "FileWriteReadback", effect.Target, now)
	if err != nil {
		return domain.Effect{}, fault.New("file_effect.complete", fault.Internal, err)
	}
	return e.store.CompleteEffectExecution(ctx, effect.ID, observed, evidence, now)
}

func (e *FileEffectEngine) authorize(grant domain.CapabilityGrant, runID string, step domain.Step, now time.Time) error {
	if grant.RunID != runID || grant.WorkerID != e.workerID || grant.Capability != domain.FilesystemWriteCapability || grant.EffectClass != domain.EffectReversibleWrite {
		return errors.New("capability grant does not authorize this run, worker, capability, and effect class")
	}
	if !grant.ActiveAt(now) {
		return errors.New("capability grant is not active")
	}
	for _, requirement := range step.RequiredCapabilities {
		if requirement.Name == grant.Capability && requirement.Scope == grant.Scope {
			return nil
		}
	}
	return errors.New("PlanIR step does not require the granted capability and scope")
}

func fileWriteStep(plan domain.Plan, stepID string) (domain.Step, error) {
	for _, step := range plan.Steps {
		if step.ID != stepID {
			continue
		}
		for _, effectClass := range step.ExpectedEffects {
			if effectClass == domain.EffectReversibleWrite {
				return step, nil
			}
		}
		return domain.Step{}, fmt.Errorf("step %q does not declare a reversible write", stepID)
	}
	return domain.Step{}, fmt.Errorf("plan %q does not contain step %q", plan.ID, stepID)
}

func sameStagedRequest(existing, proposed domain.Effect) bool {
	return existing.RunID == proposed.RunID && existing.PlanID == proposed.PlanID && existing.StepID == proposed.StepID &&
		existing.CapabilityGrantID == proposed.CapabilityGrantID && existing.Target == proposed.Target &&
		existing.DesiredArtifactID == proposed.DesiredArtifactID && existing.PreviousArtifactID == proposed.PreviousArtifactID &&
		existing.PreviousExisted == proposed.PreviousExisted && existing.Class == proposed.Class
}

func sameIdempotentRequest(existing domain.Effect, request FileWriteRequest, target, desiredArtifactID string) bool {
	return existing.RunID == request.RunID && existing.PlanID == request.PlanID && existing.StepID == request.StepID &&
		existing.CapabilityGrantID == request.CapabilityGrantID && existing.Target == target &&
		existing.DesiredArtifactID == desiredArtifactID && existing.Class == domain.EffectReversibleWrite
}

func matchesPrevious(observed *domain.Artifact, exists bool, previous *domain.Artifact, previousExisted bool) bool {
	if previousExisted {
		return exists && observed != nil && previous != nil && observed.ID == previous.ID
	}
	return !exists
}

type FileVerifier struct {
	store        EffectStore
	now          func() time.Time
	maxFileBytes int64
}

func NewFileVerifier(store EffectStore) (*FileVerifier, error) {
	if store == nil {
		return nil, fmt.Errorf("verification store is required")
	}
	return &FileVerifier{store: store, now: time.Now, maxFileBytes: DefaultMaxFileBytes}, nil
}

func (v *FileVerifier) Verify(ctx context.Context, effectID string) (domain.Effect, domain.Verification, error) {
	const op = "file_verifier.verify"
	effect, err := v.store.Effect(ctx, effectID)
	if err != nil {
		return domain.Effect{}, domain.Verification{}, err
	}
	if effect.Status == domain.EffectCommitted {
		verifications, err := v.store.VerificationsForEffect(ctx, effect.ID)
		if err != nil {
			return domain.Effect{}, domain.Verification{}, err
		}
		if len(verifications) == 0 {
			return domain.Effect{}, domain.Verification{}, fault.New(op, fault.Internal, errors.New("committed effect has no verification"))
		}
		return effect, verifications[len(verifications)-1], nil
	}
	if effect.Status != domain.EffectExecuted {
		return domain.Effect{}, domain.Verification{}, fault.New(op, fault.Conflict, fmt.Errorf("effect %q cannot be verified from %s", effect.ID, effect.Status))
	}
	grant, err := v.store.CapabilityGrant(ctx, effect.CapabilityGrantID)
	if err != nil {
		return domain.Effect{}, domain.Verification{}, err
	}
	target, err := resolveScopedTarget(grant.Scope, effect.Target)
	if err != nil {
		return domain.Effect{}, domain.Verification{}, fault.New(op, fault.Invalid, fmt.Errorf("effect target is outside its recorded scope: %w", err))
	}
	if target != effect.Target {
		return domain.Effect{}, domain.Verification{}, fault.New(op, fault.Invalid, errors.New("effect target no longer resolves to its staged path"))
	}

	now := v.now().UTC()
	observed, exists, readErr := observeFile(effect.Target, v.maxFileBytes)
	if readErr != nil {
		return domain.Effect{}, domain.Verification{}, fault.New(op, fault.Internal, fmt.Errorf("independently read target: %w", readErr))
	}
	status := domain.VerificationFailed
	kind := "FileAbsent"
	if exists {
		kind = "FileHash"
		if observed.ID == effect.DesiredArtifactID {
			status = domain.VerificationPassed
		}
	} else {
		absent, err := artifact.FromBytes([]byte(`{"exists":false}`), "application/vnd.azper.file-observation+json")
		if err != nil {
			return domain.Effect{}, domain.Verification{}, fault.New(op, fault.Internal, err)
		}
		observed = &absent
	}
	evidence, err := domain.NewEvidence(effect.ID, observed.ID, kind, effect.Target, now)
	if err != nil {
		return domain.Effect{}, domain.Verification{}, fault.New(op, fault.Internal, err)
	}
	verification, err := domain.NewVerification(effect.ID, evidence.ID, "independent BLAKE3-256 filesystem read", status, now)
	if err != nil {
		return domain.Effect{}, domain.Verification{}, fault.New(op, fault.Internal, err)
	}
	verifiedEffect, err := v.store.RecordVerification(ctx, effect.ID, *observed, evidence, verification, now)
	if err != nil {
		return domain.Effect{}, domain.Verification{}, err
	}
	if status == domain.VerificationFailed {
		return verifiedEffect, verification, fault.New(op, fault.Conflict, errors.New("target content did not satisfy the desired artifact hash"))
	}
	return verifiedEffect, verification, nil
}

func resolveScopedTarget(scope, target string) (string, error) {
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(target) == "" {
		return "", errors.New("scope and target are required")
	}
	absScope, err := filepath.Abs(scope)
	if err != nil {
		return "", fmt.Errorf("resolve capability scope: %w", err)
	}
	resolvedScope, err := filepath.EvalSymlinks(absScope)
	if err != nil {
		return "", fmt.Errorf("resolve capability scope symlinks: %w", err)
	}
	info, err := os.Stat(resolvedScope)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("capability scope is not a directory")
	}
	absTarget := target
	if !filepath.IsAbs(absTarget) {
		absTarget = filepath.Join(resolvedScope, absTarget)
	}
	absTarget, err = filepath.Abs(absTarget)
	if err != nil {
		return "", fmt.Errorf("resolve target: %w", err)
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(absTarget))
	if err != nil {
		return "", fmt.Errorf("resolve target parent: %w", err)
	}
	resolvedTarget := filepath.Join(resolvedParent, filepath.Base(absTarget))
	relative, err := filepath.Rel(resolvedScope, resolvedTarget)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("target %q is outside capability scope %q", target, scope)
	}
	if info, err := os.Lstat(resolvedTarget); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symbolic-link targets are not writable")
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("target is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect target: %w", err)
	}
	return resolvedTarget, nil
}

func observeFile(path string, maxBytes int64) (*domain.Artifact, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, false, errors.New("target is not a regular non-symlink file")
	}
	if info.Size() > maxBytes {
		return nil, false, fmt.Errorf("target exceeds %d byte limit", maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	observed, err := artifact.FromBytes(data, "application/octet-stream")
	if err != nil {
		return nil, false, err
	}
	return &observed, true, nil
}

func atomicWrite(ctx context.Context, target string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(target)
	temporary, err := os.CreateTemp(directory, ".azper-stage-*")
	if err != nil {
		return fmt.Errorf("create staged file: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()

	mode := os.FileMode(0o600)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect target mode: %w", err)
	}
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set staged file mode: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write staged file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync staged file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close staged file: %w", err)
	}
	closed = true
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return fmt.Errorf("replace target atomically: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open target directory for sync: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync target directory: %w", err)
	}
	return nil
}
