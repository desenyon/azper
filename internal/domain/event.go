package domain

import (
	"encoding/json"
	"time"
)

const (
	EventContractCreated        = "ContractCreated"
	EventRunStarted             = "RunStarted"
	EventPlanCreated            = "PlanCreated"
	EventCapabilityGranted      = "CapabilityGranted"
	EventEffectStaged           = "EffectStaged"
	EventEffectExecutionStarted = "EffectExecutionStarted"
	EventEffectExecuted         = "EffectExecuted"
	EventEffectAmbiguous        = "EffectAmbiguous"
	EventEvidenceRecorded       = "EvidenceRecorded"
	EventVerificationPassed     = "VerificationPassed"
	EventVerificationFailed     = "VerificationFailed"
	EventEffectCommitted        = "EffectCommitted"
	EventCompensationStaged     = "CompensationStaged"
	EventCompensationStarted    = "CompensationExecutionStarted"
	EventCompensationExecuted   = "CompensationExecuted"
	EventCompensationAmbiguous  = "CompensationAmbiguous"
	EventCompensationEvidence   = "CompensationEvidenceRecorded"
	EventCompensationVerified   = "CompensationVerificationPassed"
	EventCompensationFailed     = "CompensationVerificationFailed"
	EventEffectCompensated      = "EffectCompensated"
)

type Event struct {
	Sequence      int64           `json:"sequence"`
	ID            string          `json:"id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	Type          string          `json:"type"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}
