package domain

import (
	"fmt"
	"strings"
	"time"

	"azper/internal/identity"
)

const FilesystemWriteCapability = "filesystem.write"

type EffectStatus string

const (
	EffectStaged    EffectStatus = "Staged"
	EffectExecuting EffectStatus = "Executing"
	EffectExecuted  EffectStatus = "Executed"
	EffectCommitted EffectStatus = "Committed"
	EffectAmbiguous EffectStatus = "Ambiguous"
	EffectFailed    EffectStatus = "Failed"
)

type VerificationStatus string

const (
	VerificationPassed VerificationStatus = "Passed"
	VerificationFailed VerificationStatus = "Failed"
)

type CapabilityGrant struct {
	ID             string      `json:"id"`
	RunID          string      `json:"run_id"`
	WorkerID       string      `json:"worker_id"`
	Capability     string      `json:"capability"`
	Scope          string      `json:"scope"`
	EffectClass    EffectClass `json:"effect_class"`
	ApprovalSource string      `json:"approval_source"`
	GrantedAt      time.Time   `json:"granted_at"`
	ExpiresAt      time.Time   `json:"expires_at"`
}

func NewCapabilityGrant(runID, workerID, capability, scope string, effectClass EffectClass, approvalSource string, grantedAt, expiresAt time.Time) (CapabilityGrant, error) {
	id, err := identity.New("cap", grantedAt)
	if err != nil {
		return CapabilityGrant{}, fmt.Errorf("create capability grant identifier: %w", err)
	}
	grant := CapabilityGrant{
		ID:             id,
		RunID:          strings.TrimSpace(runID),
		WorkerID:       strings.TrimSpace(workerID),
		Capability:     strings.TrimSpace(capability),
		Scope:          strings.TrimSpace(scope),
		EffectClass:    effectClass,
		ApprovalSource: strings.TrimSpace(approvalSource),
		GrantedAt:      grantedAt.UTC(),
		ExpiresAt:      expiresAt.UTC(),
	}
	if err := grant.Validate(); err != nil {
		return CapabilityGrant{}, err
	}
	return grant, nil
}

func (g CapabilityGrant) Validate() error {
	if strings.TrimSpace(g.ID) == "" || strings.TrimSpace(g.RunID) == "" || strings.TrimSpace(g.WorkerID) == "" {
		return fmt.Errorf("capability grant id, run id, and worker id are required")
	}
	if strings.TrimSpace(g.Capability) == "" || strings.TrimSpace(g.Scope) == "" {
		return fmt.Errorf("capability name and scope are required")
	}
	if !validEffect(g.EffectClass) || g.EffectClass == EffectPure || g.EffectClass == EffectRead {
		return fmt.Errorf("capability grant requires a mutating effect class")
	}
	if strings.TrimSpace(g.ApprovalSource) == "" {
		return fmt.Errorf("capability approval source is required")
	}
	if g.GrantedAt.IsZero() || !g.ExpiresAt.After(g.GrantedAt) {
		return fmt.Errorf("capability expiration must follow grant time")
	}
	return nil
}

func (g CapabilityGrant) ActiveAt(now time.Time) bool {
	return !now.Before(g.GrantedAt) && now.Before(g.ExpiresAt)
}

type Artifact struct {
	ID        string `json:"id"`
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	Data      []byte `json:"-"`
}

func (a Artifact) Validate() error {
	if strings.TrimSpace(a.ID) == "" || a.Algorithm != "blake3-256" || len(a.Digest) != 64 {
		return fmt.Errorf("artifact requires a BLAKE3-256 content address")
	}
	if strings.TrimSpace(a.MediaType) == "" || a.Size < 0 || int64(len(a.Data)) != a.Size {
		return fmt.Errorf("artifact media type, size, and data must agree")
	}
	return nil
}

type Effect struct {
	ID                 string       `json:"id"`
	RunID              string       `json:"run_id"`
	PlanID             string       `json:"plan_id"`
	StepID             string       `json:"step_id"`
	CapabilityGrantID  string       `json:"capability_grant_id"`
	IdempotencyKey     string       `json:"idempotency_key"`
	Class              EffectClass  `json:"class"`
	Status             EffectStatus `json:"status"`
	Target             string       `json:"target"`
	DesiredArtifactID  string       `json:"desired_artifact_id"`
	PreviousArtifactID string       `json:"previous_artifact_id,omitempty"`
	PreviousExisted    bool         `json:"previous_existed"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

func NewFileWriteEffect(runID, planID, stepID, grantID, idempotencyKey, target string, desired Artifact, previous *Artifact, now time.Time) (Effect, error) {
	id, err := identity.New("eff", now)
	if err != nil {
		return Effect{}, fmt.Errorf("create effect identifier: %w", err)
	}
	effect := Effect{
		ID:                id,
		RunID:             strings.TrimSpace(runID),
		PlanID:            strings.TrimSpace(planID),
		StepID:            strings.TrimSpace(stepID),
		CapabilityGrantID: strings.TrimSpace(grantID),
		IdempotencyKey:    strings.TrimSpace(idempotencyKey),
		Class:             EffectReversibleWrite,
		Status:            EffectStaged,
		Target:            strings.TrimSpace(target),
		DesiredArtifactID: desired.ID,
		CreatedAt:         now.UTC(),
		UpdatedAt:         now.UTC(),
	}
	if previous != nil {
		effect.PreviousArtifactID = previous.ID
		effect.PreviousExisted = true
	}
	if err := effect.Validate(); err != nil {
		return Effect{}, err
	}
	return effect, nil
}

func (e Effect) Validate() error {
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.RunID) == "" || strings.TrimSpace(e.PlanID) == "" || strings.TrimSpace(e.StepID) == "" {
		return fmt.Errorf("effect id, run id, plan id, and step id are required")
	}
	if strings.TrimSpace(e.CapabilityGrantID) == "" || strings.TrimSpace(e.IdempotencyKey) == "" {
		return fmt.Errorf("effect capability grant and idempotency key are required")
	}
	if e.Class != EffectReversibleWrite {
		return fmt.Errorf("file effect must be a reversible write")
	}
	switch e.Status {
	case EffectStaged, EffectExecuting, EffectExecuted, EffectCommitted, EffectAmbiguous, EffectFailed:
	default:
		return fmt.Errorf("invalid effect status %q", e.Status)
	}
	if strings.TrimSpace(e.Target) == "" || strings.TrimSpace(e.DesiredArtifactID) == "" {
		return fmt.Errorf("effect target and desired artifact are required")
	}
	if e.PreviousExisted != (e.PreviousArtifactID != "") {
		return fmt.Errorf("effect previous-state fields disagree")
	}
	if e.CreatedAt.IsZero() || e.UpdatedAt.Before(e.CreatedAt) {
		return fmt.Errorf("effect timestamps are invalid")
	}
	return nil
}

type Evidence struct {
	ID         string    `json:"id"`
	EffectID   string    `json:"effect_id"`
	ArtifactID string    `json:"artifact_id"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observed_at"`
}

func NewEvidence(effectID, artifactID, kind, source string, observedAt time.Time) (Evidence, error) {
	id, err := identity.New("evd", observedAt)
	if err != nil {
		return Evidence{}, fmt.Errorf("create evidence identifier: %w", err)
	}
	evidence := Evidence{
		ID: id, EffectID: strings.TrimSpace(effectID), ArtifactID: strings.TrimSpace(artifactID),
		Kind: strings.TrimSpace(kind), Source: strings.TrimSpace(source), ObservedAt: observedAt.UTC(),
	}
	if evidence.EffectID == "" || evidence.ArtifactID == "" || evidence.Kind == "" || evidence.Source == "" || evidence.ObservedAt.IsZero() {
		return Evidence{}, fmt.Errorf("evidence requires effect, artifact, kind, source, and observation time")
	}
	return evidence, nil
}

type Verification struct {
	ID         string             `json:"id"`
	EffectID   string             `json:"effect_id"`
	EvidenceID string             `json:"evidence_id"`
	Method     string             `json:"method"`
	Status     VerificationStatus `json:"status"`
	ObservedAt time.Time          `json:"observed_at"`
}

func NewVerification(effectID, evidenceID, method string, status VerificationStatus, observedAt time.Time) (Verification, error) {
	id, err := identity.New("ver", observedAt)
	if err != nil {
		return Verification{}, fmt.Errorf("create verification identifier: %w", err)
	}
	verification := Verification{
		ID: id, EffectID: strings.TrimSpace(effectID), EvidenceID: strings.TrimSpace(evidenceID),
		Method: strings.TrimSpace(method), Status: status, ObservedAt: observedAt.UTC(),
	}
	if verification.EffectID == "" || verification.EvidenceID == "" || verification.Method == "" || verification.ObservedAt.IsZero() {
		return Verification{}, fmt.Errorf("verification requires effect, evidence, method, and observation time")
	}
	if status != VerificationPassed && status != VerificationFailed {
		return Verification{}, fmt.Errorf("invalid verification status %q", status)
	}
	return verification, nil
}
