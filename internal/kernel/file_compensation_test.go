package kernel

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"azper/internal/domain"
	"azper/internal/fault"
	"azper/internal/store/sqlite"
)

func TestFileCompensationRestoresPreviousArtifactAndSurvivesRestart(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	effect := commitHarnessEffect(t, h)
	engine, verifier, grant := newCompensationHarness(t, h)

	compensation, created, err := engine.Stage(context.Background(), effect.ID, grant.ID)
	if err != nil || !created {
		t.Fatalf("stage compensation: created=%v err=%v", created, err)
	}
	compensation, err = engine.Execute(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if compensation.Status != domain.CompensationExecuted {
		t.Fatalf("status = %s, want Executed", compensation.Status)
	}
	assertFileContent(t, h.target, "before")
	info, err := os.Stat(h.target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("restored mode = %o, want 640", info.Mode().Perm())
	}
	compensation, verification, err := verifier.Verify(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if compensation.Status != domain.CompensationCompensated || verification.Status != domain.VerificationPassed {
		t.Fatalf("compensation = %s verification = %s", compensation.Status, verification.Status)
	}

	retried, created, err := engine.Stage(context.Background(), effect.ID, grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created || retried.ID != compensation.ID || retried.Status != domain.CompensationCompensated {
		t.Fatalf("idempotent compensation changed: %#v created=%v", retried, created)
	}
	if _, second, err := verifier.Verify(context.Background(), compensation.ID); err != nil || second.ID != verification.ID {
		t.Fatalf("idempotent verification = %#v err=%v", second, err)
	}

	id := compensation.ID
	h.close(t)
	reopened, err := sqlite.Open(context.Background(), h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.Compensation(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.CompensationCompensated {
		t.Fatalf("persisted status = %s", persisted.Status)
	}
}

func TestFileCompensationRemovesFileCreatedByEffect(t *testing.T) {
	h := newFileHarness(t, nil)
	defer h.close(t)
	effect := commitHarnessEffect(t, h)
	engine, verifier, grant := newCompensationHarness(t, h)
	compensation, _, err := engine.Stage(context.Background(), effect.ID, grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !compensation.RemoveTarget {
		t.Fatal("new-file compensation should remove the target")
	}
	compensation, err = engine.Execute(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target still exists after compensation: %v", err)
	}
	compensation, verification, err := verifier.Verify(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if compensation.Status != domain.CompensationCompensated || verification.Status != domain.VerificationPassed {
		t.Fatalf("compensation = %s verification = %s", compensation.Status, verification.Status)
	}
}

func TestFileCompensationRefusesToOverwriteDrift(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect := commitHarnessEffect(t, h)
	engine, _, grant := newCompensationHarness(t, h)
	compensation, _, err := engine.Stage(context.Background(), effect.ID, grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.target, []byte("post-effect drift"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), compensation.ID); !fault.IsCategory(err, fault.Ambiguous) {
		t.Fatalf("execute error = %v, want ambiguous", err)
	}
	persisted, err := h.store.Compensation(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.CompensationAmbiguous {
		t.Fatalf("status = %s, want Ambiguous", persisted.Status)
	}
	assertFileContent(t, h.target, "post-effect drift")
}

func TestFileCompensationRecoversAfterEvidencePersistenceFailure(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect := commitHarnessEffect(t, h)
	engine, _, grant := newCompensationHarness(t, h)
	compensation, _, err := engine.Stage(context.Background(), effect.ID, grant.ID)
	if err != nil {
		t.Fatal(err)
	}

	injectionDB, err := sql.Open("sqlite", h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer injectionDB.Close()
	if _, err := injectionDB.Exec(`
		CREATE TRIGGER reject_compensation_evidence
		BEFORE INSERT ON compensation_evidence
		BEGIN
			SELECT RAISE(ABORT, 'injected compensation evidence failure');
		END
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Execute(context.Background(), compensation.ID); err == nil {
		t.Fatal("expected injected compensation evidence failure")
	}
	assertFileContent(t, h.target, "before")
	persisted, err := h.store.Compensation(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.CompensationExecuting {
		t.Fatalf("status = %s, want Executing", persisted.Status)
	}
	if _, err := injectionDB.Exec(`DROP TRIGGER reject_compensation_evidence`); err != nil {
		t.Fatal(err)
	}
	persisted, err = engine.Execute(context.Background(), compensation.ID)
	if err != nil {
		t.Fatalf("reconcile already-applied compensation: %v", err)
	}
	if persisted.Status != domain.CompensationExecuted {
		t.Fatalf("reconciled status = %s, want Executed", persisted.Status)
	}
}

func TestFileCompensationVerifierRejectsDriftAfterExecution(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect := commitHarnessEffect(t, h)
	engine, verifier, grant := newCompensationHarness(t, h)
	compensation, _, err := engine.Stage(context.Background(), effect.ID, grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	compensation, err = engine.Execute(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.target, []byte("drift after compensation"), 0o640); err != nil {
		t.Fatal(err)
	}
	failed, verification, err := verifier.Verify(context.Background(), compensation.ID)
	if !fault.IsCategory(err, fault.Conflict) {
		t.Fatalf("verify error = %v, want conflict", err)
	}
	if failed.Status != domain.CompensationFailed || verification.Status != domain.VerificationFailed {
		t.Fatalf("compensation = %s verification = %s", failed.Status, verification.Status)
	}
}

func TestCompensatedRetryDetectsLaterDrift(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect := commitHarnessEffect(t, h)
	engine, verifier, grant := newCompensationHarness(t, h)
	compensation, _, err := engine.Stage(context.Background(), effect.ID, grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	compensation, err = engine.Execute(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	compensation, verification, err := verifier.Verify(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.target, []byte("later drift"), 0o640); err != nil {
		t.Fatal(err)
	}
	persisted, historical, err := verifier.Verify(context.Background(), compensation.ID)
	if !fault.IsCategory(err, fault.Conflict) {
		t.Fatalf("recheck error = %v, want conflict", err)
	}
	if persisted.Status != domain.CompensationCompensated || historical.ID != verification.ID {
		t.Fatalf("historical compensation changed: %#v verification=%#v", persisted, historical)
	}
}

func TestExpiredCompensationGrantAllowsReadOnlyReconciliationOnly(t *testing.T) {
	t.Run("already restored", func(t *testing.T) {
		h := newFileHarness(t, stringPointer("before"))
		defer h.close(t)
		effect := commitHarnessEffect(t, h)
		engine, _, grant := newCompensationHarness(t, h)
		compensation, _, err := engine.Stage(context.Background(), effect.ID, grant.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.store.BeginCompensationExecution(context.Background(), compensation.ID, h.now.Add(5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(h.target, []byte("before"), 0o640); err != nil {
			t.Fatal(err)
		}
		engine.now = func() time.Time { return h.now.Add(3 * time.Hour) }
		compensation, err = engine.Execute(context.Background(), compensation.ID)
		if err != nil || compensation.Status != domain.CompensationExecuted {
			t.Fatalf("read-only reconcile = %#v err=%v", compensation, err)
		}
	})

	t.Run("mutation blocked", func(t *testing.T) {
		h := newFileHarness(t, stringPointer("before"))
		defer h.close(t)
		effect := commitHarnessEffect(t, h)
		engine, _, grant := newCompensationHarness(t, h)
		compensation, _, err := engine.Stage(context.Background(), effect.ID, grant.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.store.BeginCompensationExecution(context.Background(), compensation.ID, h.now.Add(5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		engine.now = func() time.Time { return h.now.Add(3 * time.Hour) }
		if _, err := engine.Execute(context.Background(), compensation.ID); !fault.IsCategory(err, fault.Invalid) {
			t.Fatalf("expired mutation error = %v, want invalid", err)
		}
		assertFileContent(t, h.target, "after")
	})
}

func TestCancelledCompensationDoesNotMutateTarget(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect := commitHarnessEffect(t, h)
	engine, _, grant := newCompensationHarness(t, h)
	compensation, _, err := engine.Stage(context.Background(), effect.ID, grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.Execute(ctx, compensation.ID); err == nil {
		t.Fatal("expected cancelled compensation")
	}
	assertFileContent(t, h.target, "after")
	persisted, err := h.store.Compensation(context.Background(), compensation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.CompensationStaged {
		t.Fatalf("status = %s, want Staged", persisted.Status)
	}
}

func commitHarnessEffect(t *testing.T, h *fileHarness) domain.Effect {
	t.Helper()
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	effect, err = h.engine.Execute(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	effect, _, err = h.verifier.Verify(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	return effect
}

func newCompensationHarness(t *testing.T, h *fileHarness) (*FileCompensationEngine, *FileCompensationVerifier, domain.CapabilityGrant) {
	t.Helper()
	grant, err := domain.NewCapabilityGrant(h.request.RunID, "compensator", domain.FilesystemWriteCapability, h.scope,
		domain.EffectReversibleWrite, "explicit test undo", h.now.Add(3*time.Minute), h.now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.GrantCapability(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	engine, err := NewFileCompensationEngine(h.store, "compensator")
	if err != nil {
		t.Fatal(err)
	}
	engine.now = func() time.Time { return h.now.Add(4 * time.Minute) }
	verifier, err := NewFileCompensationVerifier(h.store)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return h.now.Add(5 * time.Minute) }
	return engine, verifier, grant
}
