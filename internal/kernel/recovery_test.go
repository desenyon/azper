package kernel

import (
	"context"
	"os"
	"testing"
	"time"

	"azper/internal/domain"
	"azper/internal/fault"
	"azper/internal/store/sqlite"
)

func TestRecoveryCommitsAlreadyAppliedEffectAfterRestart(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
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
	h.close(t)

	store, err := sqlite.Open(context.Background(), h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recovery, err := NewRecoveryEngine(store, "kernel")
	if err != nil {
		t.Fatal(err)
	}
	recovery.executor.now = func() time.Time { return h.now.Add(2 * time.Hour) }
	recovery.verifier.now = func() time.Time { return h.now.Add(2*time.Hour + time.Minute) }
	report, err := recovery.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Inspected != 1 || report.Committed != 1 || report.NeedsAttention != 0 {
		t.Fatalf("unexpected recovery report: %#v", report)
	}
	persisted, err := store.Effect(context.Background(), effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.EffectCommitted {
		t.Fatalf("recovered effect status = %s", persisted.Status)
	}
}

func TestRecoveryReportsExpiredMutationWithoutChangingTarget(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.BeginEffectExecution(context.Background(), effect.ID, h.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	recovery, err := NewRecoveryEngine(h.store, "kernel")
	if err != nil {
		t.Fatal(err)
	}
	recovery.executor.now = func() time.Time { return h.now.Add(2 * time.Hour) }
	report, err := recovery.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Inspected != 1 || report.Committed != 0 || report.NeedsAttention != 1 {
		t.Fatalf("unexpected recovery report: %#v", report)
	}
	if report.Outcomes[0].Category != fault.Invalid || report.Outcomes[0].Status != domain.EffectExecuting {
		t.Fatalf("unexpected recovery outcome: %#v", report.Outcomes[0])
	}
	assertFileContent(t, h.target, "before")
}

func TestRecoveryPersistsAmbiguousDriftForAttention(t *testing.T) {
	h := newFileHarness(t, stringPointer("before"))
	defer h.close(t)
	effect, _, err := h.engine.Stage(context.Background(), h.request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.BeginEffectExecution(context.Background(), effect.ID, h.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.target, []byte("external drift"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovery, err := NewRecoveryEngine(h.store, "kernel")
	if err != nil {
		t.Fatal(err)
	}
	recovery.executor.now = func() time.Time { return h.now.Add(2 * time.Minute) }
	report, err := recovery.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.NeedsAttention != 1 || report.Outcomes[0].Category != fault.Ambiguous || report.Outcomes[0].Status != domain.EffectAmbiguous {
		t.Fatalf("unexpected recovery report: %#v", report)
	}
	assertFileContent(t, h.target, "external drift")
}

func TestRecoveryWithNoExecutingEffectsIsEmpty(t *testing.T) {
	h := newFileHarness(t, nil)
	defer h.close(t)
	recovery, err := NewRecoveryEngine(h.store, "kernel")
	if err != nil {
		t.Fatal(err)
	}
	report, err := recovery.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Inspected != 0 || len(report.Outcomes) != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
}
