package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"azper/internal/domain"
	"azper/internal/fault"
)

func TestContractAndRunSurviveRestartWithEvents(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "azper.db")
	store := openTestStore(t, path)

	contract := testContract(t, "persist contract")
	if err := store.CreateContract(ctx, contract); err != nil {
		t.Fatal(err)
	}
	run, err := domain.NewRun(contract.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, path)
	defer store.Close()
	gotContract, err := store.Contract(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotContract.Objective != contract.Objective {
		t.Fatalf("objective = %q, want %q", gotContract.Objective, contract.Objective)
	}
	gotRun, err := store.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRun.Status != domain.RunRunning || gotRun.ContractID != contract.ID {
		t.Fatalf("unexpected run after restart: %#v", gotRun)
	}
	events, err := store.Events(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Type != domain.EventContractCreated || events[1].Type != domain.EventRunStarted {
		t.Fatalf("unexpected event order: %#v", events)
	}
}

func TestCreateContractRollsBackWhenEventWriteFails(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "azper.db"))
	defer store.Close()

	if _, err := store.db.ExecContext(ctx, `
		CREATE TEMP TRIGGER reject_events
		BEFORE INSERT ON events
		BEGIN
			SELECT RAISE(ABORT, 'event write rejected');
		END
	`); err != nil {
		t.Fatal(err)
	}
	contract := testContract(t, "must be atomic")
	if err := store.CreateContract(ctx, contract); err == nil {
		t.Fatal("expected event insertion failure")
	}
	if _, err := store.Contract(ctx, contract.ID); !fault.IsCategory(err, fault.NotFound) {
		t.Fatalf("contract should have rolled back, got %v", err)
	}
}

func TestStartRunRejectsUnknownContractWithoutEvent(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "azper.db"))
	defer store.Close()

	run, err := domain.NewRun("ctr_missing", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	err = store.StartRun(ctx, run)
	if !fault.IsCategory(err, fault.NotFound) {
		t.Fatalf("error = %v, want not_found", err)
	}
	events, err := store.Events(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %d, want 0", len(events))
	}
}

func TestCancelledCreateDoesNotPersist(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "azper.db"))
	defer store.Close()
	contract := testContract(t, "cancel cleanly")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := store.CreateContract(ctx, contract)
	if !fault.IsCategory(err, fault.Cancelled) {
		t.Fatalf("error = %v, want cancelled", err)
	}
	if _, err := store.Contract(context.Background(), contract.ID); !fault.IsCategory(err, fault.NotFound) {
		t.Fatalf("contract should not exist, got %v", err)
	}
}

func TestDuplicateContractDoesNotDuplicateEvent(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "azper.db"))
	defer store.Close()
	contract := testContract(t, "one event only")

	if err := store.CreateContract(ctx, contract); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateContract(ctx, contract); !fault.IsCategory(err, fault.Conflict) {
		t.Fatalf("error = %v, want conflict", err)
	}
	events, err := store.Events(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
}

func TestPlanSurvivesRestartWithEvent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "azper.db")
	store := openTestStore(t, path)
	contract := testContract(t, "persist validated plan")
	if err := store.CreateContract(ctx, contract); err != nil {
		t.Fatal(err)
	}
	plan := testPlan(t, contract.ID)
	if err := store.CreatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, path)
	defer store.Close()
	got, err := store.Plan(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContractID != contract.ID || len(got.Steps) != 1 {
		t.Fatalf("unexpected persisted plan: %#v", got)
	}
	events, err := store.Events(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != domain.EventPlanCreated {
		t.Fatalf("unexpected plan events: %#v", events)
	}
}

func TestCreatePlanRollsBackWhenEventWriteFails(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "azper.db"))
	defer store.Close()
	contract := testContract(t, "own a plan")
	if err := store.CreateContract(ctx, contract); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
		CREATE TEMP TRIGGER reject_plan_event
		BEFORE INSERT ON events
		BEGIN
			SELECT RAISE(ABORT, 'event write rejected');
		END
	`); err != nil {
		t.Fatal(err)
	}
	plan := testPlan(t, contract.ID)
	if err := store.CreatePlan(ctx, plan); err == nil {
		t.Fatal("expected event insertion failure")
	}
	if _, err := store.Plan(ctx, plan.ID); !fault.IsCategory(err, fault.NotFound) {
		t.Fatalf("plan should have rolled back, got %v", err)
	}
}

func TestMigrationFromVersionOnePreservesContract(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "azper.db")
	store := openTestStore(t, path)
	contract := testContract(t, "survive schema upgrade")
	if err := store.CreateContract(ctx, contract); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"verifications", "evidence", "effects", "artifacts", "capability_grants", "plans"} {
		if _, err := store.db.ExecContext(ctx, `DROP TABLE `+table); err != nil {
			t.Fatalf("drop post-v1 table %s: %v", table, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, path)
	defer store.Close()
	if _, err := store.Contract(ctx, contract.ID); err != nil {
		t.Fatalf("contract lost during migration: %v", err)
	}
	plan := testPlan(t, contract.ID)
	if err := store.CreatePlan(ctx, plan); err != nil {
		t.Fatalf("version two table unavailable after migration: %v", err)
	}
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testContract(t *testing.T, objective string) domain.Contract {
	t.Helper()
	contract, err := domain.NewContract(objective, []string{"state can be read back"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func testPlan(t *testing.T, contractID string) domain.Plan {
	t.Helper()
	now := time.Now()
	milestone, err := domain.NewMilestone("observe state", nil, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	step, err := domain.NewStep(milestone.ID, "read state", now)
	if err != nil {
		t.Fatal(err)
	}
	step.Postconditions = []string{"state observed"}
	step.CandidateTools = []string{"filesystem.read"}
	step.ExpectedEffects = []domain.EffectClass{domain.EffectRead}
	step.Verification = domain.VerificationStrategy{
		Method:           "read back",
		RequiredEvidence: []string{"observed content"},
	}
	milestone.StepIDs = []string{step.ID}
	plan, err := domain.NewPlan(contractID, []domain.Milestone{milestone}, []domain.Step{step}, now)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
