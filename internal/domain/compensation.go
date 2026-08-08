package domain

import (
	"fmt"
	"strings"
	"time"

	"azper/internal/identity"
)

type CompensationStatus string

const (
	CompensationStaged      CompensationStatus = "Staged"
	CompensationExecuting   CompensationStatus = "Executing"
	CompensationExecuted    CompensationStatus = "Executed"
	CompensationCompensated CompensationStatus = "Compensated"
	CompensationAmbiguous   CompensationStatus = "Ambiguous"
	CompensationFailed      CompensationStatus = "Failed"
)

type Compensation struct {
	ID                string             `json:"id"`
	EffectID          string             `json:"effect_id"`
	CapabilityGrantID string             `json:"capability_grant_id"`
	Status            CompensationStatus `json:"status"`
	Target            string             `json:"target"`
	RestoreArtifactID string             `json:"restore_artifact_id,omitempty"`
	RemoveTarget      bool               `json:"remove_target"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

func NewCompensation(effect Effect, capabilityGrantID string, now time.Time) (Compensation, error) {
	if effect.Status != EffectCommitted {
		return Compensation{}, fmt.Errorf("effect %q is not committed", effect.ID)
	}
	id, err := identity.New("cmp", now)
	if err != nil {
		return Compensation{}, fmt.Errorf("create compensation identifier: %w", err)
	}
	compensation := Compensation{
		ID: id, EffectID: effect.ID, CapabilityGrantID: strings.TrimSpace(capabilityGrantID),
		Status: CompensationStaged, Target: effect.Target, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	if effect.PreviousExisted {
		compensation.RestoreArtifactID = effect.PreviousArtifactID
	} else {
		compensation.RemoveTarget = true
	}
	if err := compensation.Validate(); err != nil {
		return Compensation{}, err
	}
	return compensation, nil
}

func (c Compensation) Validate() error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.EffectID) == "" || strings.TrimSpace(c.CapabilityGrantID) == "" {
		return fmt.Errorf("compensation id, effect id, and capability grant id are required")
	}
	switch c.Status {
	case CompensationStaged, CompensationExecuting, CompensationExecuted, CompensationCompensated, CompensationAmbiguous, CompensationFailed:
	default:
		return fmt.Errorf("invalid compensation status %q", c.Status)
	}
	if strings.TrimSpace(c.Target) == "" {
		return fmt.Errorf("compensation target is required")
	}
	if c.RemoveTarget == (strings.TrimSpace(c.RestoreArtifactID) != "") {
		return fmt.Errorf("compensation must either restore one artifact or remove the target")
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return fmt.Errorf("compensation timestamps are invalid")
	}
	return nil
}

type CompensationEvidence struct {
	ID             string    `json:"id"`
	CompensationID string    `json:"compensation_id"`
	ArtifactID     string    `json:"artifact_id"`
	Kind           string    `json:"kind"`
	Source         string    `json:"source"`
	ObservedAt     time.Time `json:"observed_at"`
}

func NewCompensationEvidence(compensationID, artifactID, kind, source string, observedAt time.Time) (CompensationEvidence, error) {
	id, err := identity.New("cev", observedAt)
	if err != nil {
		return CompensationEvidence{}, fmt.Errorf("create compensation evidence identifier: %w", err)
	}
	evidence := CompensationEvidence{
		ID: id, CompensationID: strings.TrimSpace(compensationID), ArtifactID: strings.TrimSpace(artifactID),
		Kind: strings.TrimSpace(kind), Source: strings.TrimSpace(source), ObservedAt: observedAt.UTC(),
	}
	if evidence.CompensationID == "" || evidence.ArtifactID == "" || evidence.Kind == "" || evidence.Source == "" || evidence.ObservedAt.IsZero() {
		return CompensationEvidence{}, fmt.Errorf("compensation evidence requires compensation, artifact, kind, source, and observation time")
	}
	return evidence, nil
}

type CompensationVerification struct {
	ID             string             `json:"id"`
	CompensationID string             `json:"compensation_id"`
	EvidenceID     string             `json:"evidence_id"`
	Method         string             `json:"method"`
	Status         VerificationStatus `json:"status"`
	ObservedAt     time.Time          `json:"observed_at"`
}

func NewCompensationVerification(compensationID, evidenceID, method string, status VerificationStatus, observedAt time.Time) (CompensationVerification, error) {
	id, err := identity.New("cvr", observedAt)
	if err != nil {
		return CompensationVerification{}, fmt.Errorf("create compensation verification identifier: %w", err)
	}
	verification := CompensationVerification{
		ID: id, CompensationID: strings.TrimSpace(compensationID), EvidenceID: strings.TrimSpace(evidenceID),
		Method: strings.TrimSpace(method), Status: status, ObservedAt: observedAt.UTC(),
	}
	if verification.CompensationID == "" || verification.EvidenceID == "" || verification.Method == "" || verification.ObservedAt.IsZero() {
		return CompensationVerification{}, fmt.Errorf("compensation verification requires compensation, evidence, method, and observation time")
	}
	if status != VerificationPassed && status != VerificationFailed {
		return CompensationVerification{}, fmt.Errorf("invalid verification status %q", status)
	}
	return verification, nil
}
