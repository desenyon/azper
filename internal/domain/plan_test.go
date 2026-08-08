package domain

import (
	"strings"
	"testing"
	"time"
)

func TestPlanAcceptsObservableReadStep(t *testing.T) {
	plan := validPlan(t)
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanRejectsStepDependencyCycle(t *testing.T) {
	plan := validPlan(t)
	now := time.Now()
	second, err := NewStep(plan.Milestones[0].ID, "inspect result independently", now)
	if err != nil {
		t.Fatal(err)
	}
	second.Postconditions = []string{"result observed"}
	second.CandidateTools = []string{"filesystem.read"}
	second.ExpectedEffects = []EffectClass{EffectRead}
	second.Verification = VerificationStrategy{Method: "compare state", RequiredEvidence: []string{"file hash"}}
	plan.Steps[0].Dependencies = []string{second.ID}
	second.Dependencies = []string{plan.Steps[0].ID}
	plan.Steps = append(plan.Steps, second)
	plan.Milestones[0].StepIDs = append(plan.Milestones[0].StepIDs, second.ID)

	err = plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v, want dependency cycle", err)
	}
}

func TestPlanRejectsDanglingDependency(t *testing.T) {
	plan := validPlan(t)
	plan.Steps[0].Dependencies = []string{"stp_missing"}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "unknown step") {
		t.Fatalf("error = %v, want unknown step", err)
	}
}

func TestPlanRejectsUnverifiableStep(t *testing.T) {
	plan := validPlan(t)
	plan.Steps[0].Verification = VerificationStrategy{}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatalf("error = %v, want verification requirement", err)
	}
}

func TestPlanRejectsMutationWithoutCapability(t *testing.T) {
	plan := validPlan(t)
	step := &plan.Steps[0]
	step.ExpectedEffects = []EffectClass{EffectIrreversibleWrite}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "without a required capability") {
		t.Fatalf("error = %v, want capability requirement", err)
	}
}

func TestPlanRequiresCompensationForReversibleWrite(t *testing.T) {
	plan := validPlan(t)
	step := &plan.Steps[0]
	step.ExpectedEffects = []EffectClass{EffectReversibleWrite}
	step.RequiredCapabilities = []CapabilityRequirement{{Name: "filesystem.write", Scope: "/project/output"}}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "compensation strategy") {
		t.Fatalf("error = %v, want compensation requirement", err)
	}

	step.CompensationStrategy = "restore the staged original"
	if err := plan.Validate(); err != nil {
		t.Fatalf("valid compensated write rejected: %v", err)
	}
}

func validPlan(t *testing.T) Plan {
	t.Helper()
	now := time.Now()
	milestone, err := NewMilestone("observe current state", nil, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	step, err := NewStep(milestone.ID, "read current state", now)
	if err != nil {
		t.Fatal(err)
	}
	step.Postconditions = []string{"state was observed"}
	step.CandidateTools = []string{"filesystem.read"}
	step.ExpectedEffects = []EffectClass{EffectRead}
	step.Verification = VerificationStrategy{
		Method:           "read back state",
		RequiredEvidence: []string{"observed content"},
	}
	milestone.StepIDs = []string{step.ID}
	plan, err := NewPlan("ctr_test", []Milestone{milestone}, []Step{step}, now)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
