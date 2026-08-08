package domain

import (
	"fmt"
	"strings"
	"time"

	"azper/internal/identity"
)

type EffectClass string

const (
	EffectPure                  EffectClass = "Pure"
	EffectRead                  EffectClass = "Read"
	EffectReversibleWrite       EffectClass = "ReversibleWrite"
	EffectCompensatableWrite    EffectClass = "CompensatableWrite"
	EffectIrreversibleWrite     EffectClass = "IrreversibleWrite"
	EffectExternalCommunication EffectClass = "ExternalCommunication"
	EffectFinancial             EffectClass = "FinancialEffect"
	EffectPrivilegedSystem      EffectClass = "PrivilegedSystemAction"
)

type RiskClass string

const (
	RiskLow      RiskClass = "Low"
	RiskMedium   RiskClass = "Medium"
	RiskHigh     RiskClass = "High"
	RiskCritical RiskClass = "Critical"
)

type FailurePolicy string

const (
	FailureStop   FailurePolicy = "Stop"
	FailureReplan FailurePolicy = "Replan"
)

type CapabilityRequirement struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type VerificationStrategy struct {
	Method           string   `json:"method"`
	RequiredEvidence []string `json:"required_evidence"`
}

type Step struct {
	ID                      string                  `json:"id"`
	MilestoneID             string                  `json:"milestone_id"`
	Objective               string                  `json:"objective"`
	Dependencies            []string                `json:"dependencies"`
	Preconditions           []string                `json:"preconditions"`
	Postconditions          []string                `json:"postconditions"`
	RequiredCapabilities    []CapabilityRequirement `json:"required_capabilities"`
	CandidateTools          []string                `json:"candidate_tools"`
	ExpectedEffects         []EffectClass           `json:"expected_effects"`
	Verification            VerificationStrategy    `json:"verification"`
	CompensationStrategy    string                  `json:"compensation_strategy,omitempty"`
	ResourceLocks           []string                `json:"resource_locks"`
	EstimatedCostMicros     int64                   `json:"estimated_cost_micros"`
	EstimatedLatencyMillis  int64                   `json:"estimated_latency_millis"`
	Risk                    RiskClass               `json:"risk"`
	ParallelizationEligible bool                    `json:"parallelization_eligible"`
	FailurePolicy           FailurePolicy           `json:"failure_policy"`
}

type Milestone struct {
	ID           string   `json:"id"`
	Objective    string   `json:"objective"`
	Dependencies []string `json:"dependencies"`
	StepIDs      []string `json:"step_ids"`
}

type Plan struct {
	ID         string      `json:"id"`
	ContractID string      `json:"contract_id"`
	Milestones []Milestone `json:"milestones"`
	Steps      []Step      `json:"steps"`
	CreatedAt  time.Time   `json:"created_at"`
}

func NewPlan(contractID string, milestones []Milestone, steps []Step, now time.Time) (Plan, error) {
	id, err := identity.New("pln", now)
	if err != nil {
		return Plan{}, fmt.Errorf("create plan identifier: %w", err)
	}
	plan := Plan{
		ID:         id,
		ContractID: strings.TrimSpace(contractID),
		Milestones: milestones,
		Steps:      steps,
		CreatedAt:  now.UTC(),
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func NewMilestone(objective string, dependencies, stepIDs []string, now time.Time) (Milestone, error) {
	id, err := identity.New("mls", now)
	if err != nil {
		return Milestone{}, fmt.Errorf("create milestone identifier: %w", err)
	}
	milestone := Milestone{
		ID:           id,
		Objective:    strings.TrimSpace(objective),
		Dependencies: dependencies,
		StepIDs:      stepIDs,
	}
	if milestone.Objective == "" {
		return Milestone{}, fmt.Errorf("milestone objective is required")
	}
	return milestone, nil
}

func NewStep(milestoneID, objective string, now time.Time) (Step, error) {
	id, err := identity.New("stp", now)
	if err != nil {
		return Step{}, fmt.Errorf("create step identifier: %w", err)
	}
	if strings.TrimSpace(milestoneID) == "" {
		return Step{}, fmt.Errorf("step milestone id is required")
	}
	if strings.TrimSpace(objective) == "" {
		return Step{}, fmt.Errorf("step objective is required")
	}
	return Step{
		ID:            id,
		MilestoneID:   milestoneID,
		Objective:     strings.TrimSpace(objective),
		Risk:          RiskLow,
		FailurePolicy: FailureStop,
	}, nil
}

func (p Plan) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("plan id is required")
	}
	if strings.TrimSpace(p.ContractID) == "" {
		return fmt.Errorf("plan contract id is required")
	}
	if p.CreatedAt.IsZero() {
		return fmt.Errorf("plan creation time is required")
	}
	if len(p.Milestones) == 0 {
		return fmt.Errorf("plan requires at least one milestone")
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("plan requires at least one step")
	}

	milestoneDeps := make(map[string][]string, len(p.Milestones))
	milestoneSteps := make(map[string]string, len(p.Steps))
	for _, milestone := range p.Milestones {
		if strings.TrimSpace(milestone.ID) == "" || strings.TrimSpace(milestone.Objective) == "" {
			return fmt.Errorf("milestone id and objective are required")
		}
		if _, exists := milestoneDeps[milestone.ID]; exists {
			return fmt.Errorf("duplicate milestone id %q", milestone.ID)
		}
		if len(milestone.StepIDs) == 0 {
			return fmt.Errorf("milestone %q requires at least one step", milestone.ID)
		}
		if err := rejectDuplicateStrings("milestone dependency", milestone.Dependencies); err != nil {
			return fmt.Errorf("milestone %q: %w", milestone.ID, err)
		}
		if err := rejectDuplicateStrings("milestone step", milestone.StepIDs); err != nil {
			return fmt.Errorf("milestone %q: %w", milestone.ID, err)
		}
		milestoneDeps[milestone.ID] = milestone.Dependencies
		for _, stepID := range milestone.StepIDs {
			if owner, exists := milestoneSteps[stepID]; exists {
				return fmt.Errorf("step %q belongs to both milestone %q and %q", stepID, owner, milestone.ID)
			}
			milestoneSteps[stepID] = milestone.ID
		}
	}
	if err := validateDAG("milestone", milestoneDeps); err != nil {
		return err
	}

	stepDeps := make(map[string][]string, len(p.Steps))
	for _, step := range p.Steps {
		if err := validateStep(step, milestoneDeps); err != nil {
			return err
		}
		if _, exists := stepDeps[step.ID]; exists {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		owner, listed := milestoneSteps[step.ID]
		if !listed {
			return fmt.Errorf("step %q is not listed by a milestone", step.ID)
		}
		if owner != step.MilestoneID {
			return fmt.Errorf("step %q names milestone %q but is listed by %q", step.ID, step.MilestoneID, owner)
		}
		stepDeps[step.ID] = step.Dependencies
	}
	if len(milestoneSteps) != len(stepDeps) {
		for stepID := range milestoneSteps {
			if _, exists := stepDeps[stepID]; !exists {
				return fmt.Errorf("milestone lists unknown step %q", stepID)
			}
		}
	}
	return validateDAG("step", stepDeps)
}

func validateStep(step Step, milestones map[string][]string) error {
	if err := validateStepStructure(step, milestones); err != nil {
		return err
	}
	if err := validateCapabilityRequirements(step); err != nil {
		return err
	}
	return validateEffectPolicy(step)
}

func validateStepStructure(step Step, milestones map[string][]string) error {
	if strings.TrimSpace(step.ID) == "" || strings.TrimSpace(step.Objective) == "" {
		return fmt.Errorf("step id and objective are required")
	}
	if _, exists := milestones[step.MilestoneID]; !exists {
		return fmt.Errorf("step %q references unknown milestone %q", step.ID, step.MilestoneID)
	}
	if len(step.Postconditions) == 0 {
		return fmt.Errorf("step %q requires a postcondition", step.ID)
	}
	if len(step.CandidateTools) == 0 {
		return fmt.Errorf("step %q requires a candidate tool", step.ID)
	}
	if len(step.ExpectedEffects) == 0 {
		return fmt.Errorf("step %q requires an expected effect", step.ID)
	}
	if strings.TrimSpace(step.Verification.Method) == "" || len(step.Verification.RequiredEvidence) == 0 {
		return fmt.Errorf("step %q requires a verification method and evidence", step.ID)
	}
	if step.EstimatedCostMicros < 0 || step.EstimatedLatencyMillis < 0 {
		return fmt.Errorf("step %q estimates cannot be negative", step.ID)
	}
	if !validRisk(step.Risk) {
		return fmt.Errorf("step %q has invalid risk %q", step.ID, step.Risk)
	}
	if step.FailurePolicy != FailureStop && step.FailurePolicy != FailureReplan {
		return fmt.Errorf("step %q has invalid failure policy %q", step.ID, step.FailurePolicy)
	}
	if err := rejectDuplicateStrings("step dependency", step.Dependencies); err != nil {
		return fmt.Errorf("step %q: %w", step.ID, err)
	}
	if err := rejectDuplicateStrings("candidate tool", step.CandidateTools); err != nil {
		return fmt.Errorf("step %q: %w", step.ID, err)
	}
	if err := rejectDuplicateStrings("resource lock", step.ResourceLocks); err != nil {
		return fmt.Errorf("step %q: %w", step.ID, err)
	}
	return nil
}

func validateCapabilityRequirements(step Step) error {
	capabilities := make(map[string]struct{}, len(step.RequiredCapabilities))
	for _, capability := range step.RequiredCapabilities {
		if strings.TrimSpace(capability.Name) == "" || strings.TrimSpace(capability.Scope) == "" {
			return fmt.Errorf("step %q capability name and scope are required", step.ID)
		}
		key := capability.Name + "\x00" + capability.Scope
		if _, exists := capabilities[key]; exists {
			return fmt.Errorf("step %q has duplicate capability %q in scope %q", step.ID, capability.Name, capability.Scope)
		}
		capabilities[key] = struct{}{}
	}
	return nil
}

func validateEffectPolicy(step Step) error {
	mutation := false
	compensatable := false
	seenEffects := make(map[EffectClass]struct{}, len(step.ExpectedEffects))
	for _, effect := range step.ExpectedEffects {
		if !validEffect(effect) {
			return fmt.Errorf("step %q has invalid effect class %q", step.ID, effect)
		}
		if _, exists := seenEffects[effect]; exists {
			return fmt.Errorf("step %q has duplicate effect class %q", step.ID, effect)
		}
		seenEffects[effect] = struct{}{}
		if effect != EffectPure && effect != EffectRead {
			mutation = true
		}
		if effect == EffectReversibleWrite || effect == EffectCompensatableWrite {
			compensatable = true
		}
	}
	if mutation && len(step.RequiredCapabilities) == 0 {
		return fmt.Errorf("step %q mutates state without a required capability", step.ID)
	}
	if compensatable && strings.TrimSpace(step.CompensationStrategy) == "" {
		return fmt.Errorf("step %q requires a compensation strategy", step.ID)
	}
	return nil
}

func validateDAG(kind string, dependencies map[string][]string) error {
	for id, refs := range dependencies {
		for _, ref := range refs {
			if ref == id {
				return fmt.Errorf("%s %q depends on itself", kind, id)
			}
			if _, exists := dependencies[ref]; !exists {
				return fmt.Errorf("%s %q depends on unknown %s %q", kind, id, kind, ref)
			}
		}
	}

	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(dependencies))
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case visiting:
			return fmt.Errorf("%s dependency cycle includes %q", kind, id)
		case visited:
			return nil
		}
		state[id] = visiting
		for _, dependency := range dependencies[id] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = visited
		return nil
	}
	for id := range dependencies {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func rejectDuplicateStrings(kind string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s cannot be empty", kind)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate %s %q", kind, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validEffect(effect EffectClass) bool {
	switch effect {
	case EffectPure, EffectRead, EffectReversibleWrite, EffectCompensatableWrite,
		EffectIrreversibleWrite, EffectExternalCommunication, EffectFinancial, EffectPrivilegedSystem:
		return true
	default:
		return false
	}
}

func validRisk(risk RiskClass) bool {
	switch risk {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
		return true
	default:
		return false
	}
}
