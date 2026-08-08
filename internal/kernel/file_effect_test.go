package kernel

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"azper/internal/domain"
	"azper/internal/fault"
	"azper/internal/store/sqlite"
)

func TestFileEffectExecutesVerifiesAndSurvivesRestart(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	effect, created, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first staging should create an effect")
	}
	assertFileContent(t, h.target, "before")

	effect, err = h.engine.Execute(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effect.Status != domain.EffectExecuted {
		t.Fatalf("effect status = %s, want Executed", effect.Status)
	}
	assertFileContent(t, h.target, "after")

	effect, verification, err := h.verifier.Verify(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effect.Status != domain.EffectCommitted || verification.Status != domain.VerificationPassed {
		t.Fatalf("effect = %s, verification = %s", effect.Status, verification.Status)
	}
	retried, created, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatalf("idempotent retry after commit: %v", err)
	}
	if created || retried.ID != effect.ID || retried.Status != domain.EffectCommitted {
		t.Fatalf("retry created or changed effect: %#v", retried)
	}
	evidence, err := h.store.EvidenceForEffect(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 2 || evidence[0].ID == evidence[1].ID {
		t.Fatalf("expected executor and verifier evidence, got %#v", evidence)
	}

	previousID := effect.PreviousArtifactID
	h.close(t)
	reopened, err := sqlite.Open(context.Background(), h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.Effect(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.EffectCommitted {
		t.Fatalf("persisted status = %s", persisted.Status)
	}
	previous, err := reopened.Artifact(context.Background(), previousID)
	if err != nil {
		t.Fatal(err)
	}
	if string(previous.Data) != "before" {
		t.Fatalf("previous artifact = %q", previous.Data)
	}
}

func TestFileEffectIdempotencyDoesNotDuplicateEffect(t *testing.T) {
	h := newFileHarness(t, nil)
	defer h.close(t)
	first, created, err := h.engine.Stage(context.Background(), h.request)
	if err != nil || !created {
		t.Fatalf("first stage: created=%v err=%v", created, err)
	}
	second, created, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("idempotent stage created duplicate: first=%q second=%q created=%v", first.ID, second.ID, created)
	}
	events, err := h.store.Events(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != domain.EventEffectStaged {
		t.Fatalf("unexpected effect events: %#v", events)
	}

	changed := h.request
	changed.Content = []byte("different")
	if _, _, err := h.engine.Stage(context.Background(), changed); !fault.IsCategory(err, fault.Conflict) {
		t.Fatalf("changed request error = %v, want conflict", err)
	}
}

func TestFileEffectRejectsScopeEscapeAndSymlinkParent(t *testing.T) {
	h := newFileHarness(t, nil)
	defer h.close(t)
	outside := t.TempDir()
	escape := h.request
	escape.Target = filepath.Join(outside, "outside.txt")
	if _, _, err := h.engine.Stage(context.Background(), escape); !fault.IsCategory(err, fault.Invalid) {
		t.Fatalf("scope escape error = %v, want invalid", err)
	}
	if _, err := os.Stat(escape.Target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside file unexpectedly changed: %v", err)
	}

	link := filepath.Join(h.scope, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	symlinkEscape := h.request
	symlinkEscape.IdempotencyKey = "symlink-escape"
	symlinkEscape.Target = filepath.Join(link, "outside.txt")
	if _, _, err := h.engine.Stage(context.Background(), symlinkEscape); !fault.IsCategory(err, fault.Invalid) {
		t.Fatalf("symlink escape error = %v, want invalid", err)
	}
}

func TestFileEffectRejectsExpiredGrant(t *testing.T) {
	h := newFileHarness(t, nil)
	defer h.close(t)
	h.engine.now = func() time.Time { return h.now.Add(2 * time.Hour) }
	if _, _, err := h.engine.Stage(context.Background(), h.request); !fault.IsCategory(err, fault.Invalid) {
		t.Fatalf("expired grant error = %v, want invalid", err)
	}
}

func TestFileEffectMarksUnreconciledTargetAmbiguous(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.target, []byte("someone else changed it"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.Execute(context.Background(), effect.ID); !fault.IsCategory(err, fault.Ambiguous) {
		t.Fatalf("execute error = %v, want ambiguous outcome", err)
	}
	persisted, err := h.store.Effect(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.EffectAmbiguous {
		t.Fatalf("effect status = %s, want Ambiguous", persisted.Status)
	}
	assertFileContent(t, h.target, "someone else changed it")
}

func TestFileEffectReconcilesAlreadyAppliedDesiredState(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.target, h.request.Content, 0o600); err != nil {
		t.Fatal(err)
	}
	effect, err = h.engine.Execute(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effect.Status != domain.EffectExecuted {
		t.Fatalf("effect status = %s, want Executed", effect.Status)
	}
}

func TestExpiredGrantStillAllowsReadOnlyReconciliation(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.BeginEffectExecution(context.Background(), effect.ID, h.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.target, h.request.Content, 0o600); err != nil {
		t.Fatal(err)
	}
	h.engine.now = func() time.Time { return h.now.Add(2 * time.Hour) }
	effect, err = h.engine.Execute(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effect.Status != domain.EffectExecuted {
		t.Fatalf("reconciled status = %s, want Executed", effect.Status)
	}
}

func TestExpiredGrantCannotRetryMutation(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.BeginEffectExecution(context.Background(), effect.ID, h.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	h.engine.now = func() time.Time { return h.now.Add(2 * time.Hour) }
	if _, err := h.engine.Execute(context.Background(), effect.ID); !fault.IsCategory(err, fault.Invalid) {
		t.Fatalf("expired retry error = %v, want invalid", err)
	}
	assertFileContent(t, h.target, "before")
}

func TestFileEffectRecoversAfterDurableEvidenceWriteFails(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}

	injectionDB, err := sql.Open("sqlite", h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer injectionDB.Close()
	if _, err := injectionDB.Exec(`
		CREATE TRIGGER reject_execution_evidence
		BEFORE INSERT ON evidence
		BEGIN
			SELECT RAISE(ABORT, 'injected evidence failure');
		END
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.Execute(context.Background(), effect.ID); err == nil {
		t.Fatal("expected injected durable evidence failure")
	}
	assertFileContent(t, h.target, "after")
	persisted, err := h.store.Effect(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.EffectExecuting {
		t.Fatalf("status after evidence failure = %s, want Executing", persisted.Status)
	}
	if _, err := injectionDB.Exec(`DROP TRIGGER reject_execution_evidence`); err != nil {
		t.Fatal(err)
	}

	persisted, err = h.engine.Execute(context.Background(), effect.ID)
	if err != nil {
		t.Fatalf("reconcile already-applied write: %v", err)
	}
	if persisted.Status != domain.EffectExecuted {
		t.Fatalf("reconciled status = %s, want Executed", persisted.Status)
	}
}

func TestVerifierRejectsDriftAfterExecution(t *testing.T) {
	h := newFileHarness(t, nil)
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	effect, err = h.engine.Execute(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.target, []byte("drift"), 0o600); err != nil {
		t.Fatal(err)
	}
	failed, verification, err := h.verifier.Verify(context.Background(), effect.ID)
	if !fault.IsCategory(err, fault.Conflict) {
		t.Fatalf("verification error = %v, want conflict", err)
	}
	if failed.Status != domain.EffectFailed || verification.Status != domain.VerificationFailed {
		t.Fatalf("effect = %s verification = %s", failed.Status, verification.Status)
	}
}

func TestCancelledExecutionDoesNotMutateTarget(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.engine.Execute(ctx, effect.ID); err == nil {
		t.Fatal("expected cancelled execution")
	}
	assertFileContent(t, h.target, "before")
	persisted, err := h.store.Effect(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.EffectStaged {
		t.Fatalf("cancelled effect status = %s, want Staged", persisted.Status)
	}
}

type fileHarness struct {
	store    *sqlite.Store
	engine   *FileEffectEngine
	verifier *FileVerifier
	request  FileWriteRequest
	target   string
	scope    string
	dbPath   string
	now      time.Time
}

func newFileHarness(t *testing.T, initial *string) *fileHarness {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 8, 20, 0, 0, 0, time.UTC)
	scope := t.TempDir()
	target := filepath.Join(scope, "result.txt")
	if initial != nil {
		if err := os.WriteFile(target, []byte(*initial), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	dbPath := filepath.Join(t.TempDir(), "azper.db")
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := domain.NewContract("write a verified file", []string{"target bytes match"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateContract(ctx, contract); err != nil {
		t.Fatal(err)
	}
	run, err := domain.NewRun(contract.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	milestone, err := domain.NewMilestone("materialize requested state", nil, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	step, err := domain.NewStep(milestone.ID, "write target file", now)
	if err != nil {
		t.Fatal(err)
	}
	step.Postconditions = []string{"target bytes match desired artifact"}
	step.RequiredCapabilities = []domain.CapabilityRequirement{{Name: domain.FilesystemWriteCapability, Scope: scope}}
	step.CandidateTools = []string{"filesystem.write"}
	step.ExpectedEffects = []domain.EffectClass{domain.EffectReversibleWrite}
	step.Verification = domain.VerificationStrategy{Method: "independent file hash", RequiredEvidence: []string{"BLAKE3-256 digest"}}
	step.CompensationStrategy = "restore the previous staged artifact"
	step.ResourceLocks = []string{target}
	step.Risk = domain.RiskMedium
	step.FailurePolicy = domain.FailureReplan
	milestone.StepIDs = []string{step.ID}
	plan, err := domain.NewPlan(contract.ID, []domain.Milestone{milestone}, []domain.Step{step}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	grant, err := domain.NewCapabilityGrant(run.ID, "kernel", domain.FilesystemWriteCapability, scope,
		domain.EffectReversibleWrite, "test owner approval", now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantCapability(ctx, grant); err != nil {
		t.Fatal(err)
	}
	engine, err := NewFileEffectEngine(store, "kernel")
	if err != nil {
		t.Fatal(err)
	}
	engine.now = func() time.Time { return now.Add(time.Minute) }
	verifier, err := NewFileVerifier(store)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return now.Add(2 * time.Minute) }
	return &fileHarness{
		store: store, engine: engine, verifier: verifier, target: target, scope: scope, dbPath: dbPath, now: now,
		request: FileWriteRequest{
			RunID: run.ID, PlanID: plan.ID, StepID: step.ID, CapabilityGrantID: grant.ID,
			IdempotencyKey: "write-result-v1", Target: target, Content: []byte("after"),
		},
	}
}

func (h *fileHarness) close(t *testing.T) {
	t.Helper()
	if h.store != nil {
		if err := h.store.Close(); err != nil {
			t.Fatal(err)
		}
		h.store = nil
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func stringPointer(value string) *string { return &value }
