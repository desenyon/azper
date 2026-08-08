package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	artifactutil "azper/internal/artifact"
	"azper/internal/domain"
	"azper/internal/fault"
)

func (s *Store) GrantCapability(ctx context.Context, grant domain.CapabilityGrant) error {
	const op = "capability.grant"
	if err := grant.Validate(); err != nil {
		return fault.New(op, fault.Invalid, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO capability_grants (
			id, run_id, worker_id, capability, scope, effect_class, approval_source,
			granted_at_unix_nano, expires_at_unix_nano
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		grant.ID, grant.RunID, grant.WorkerID, grant.Capability, grant.Scope, grant.EffectClass,
		grant.ApprovalSource, grant.GrantedAt.UnixNano(), grant.ExpiresAt.UnixNano(),
	); err != nil {
		return fault.New(op, fault.Conflict, fmt.Errorf("insert capability grant: %w", err))
	}
	if err := insertEvent(ctx, tx, "CapabilityGrant", grant.ID, domain.EventCapabilityGranted, grant.GrantedAt, grant); err != nil {
		return fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("commit capability grant and event: %w", err))
	}
	return nil
}

func (s *Store) CapabilityGrant(ctx context.Context, id string) (domain.CapabilityGrant, error) {
	const op = "capability.get"
	var grant domain.CapabilityGrant
	var grantedAt, expiresAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, run_id, worker_id, capability, scope, effect_class, approval_source,
		       granted_at_unix_nano, expires_at_unix_nano
		FROM capability_grants WHERE id = ?`, id,
	).Scan(&grant.ID, &grant.RunID, &grant.WorkerID, &grant.Capability, &grant.Scope,
		&grant.EffectClass, &grant.ApprovalSource, &grantedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CapabilityGrant{}, fault.New(op, fault.NotFound, fmt.Errorf("capability grant %q does not exist", id))
	}
	if err != nil {
		return domain.CapabilityGrant{}, fault.New(op, fault.Internal, fmt.Errorf("read capability grant: %w", err))
	}
	grant.GrantedAt = time.Unix(0, grantedAt).UTC()
	grant.ExpiresAt = time.Unix(0, expiresAt).UTC()
	if err := grant.Validate(); err != nil {
		return domain.CapabilityGrant{}, fault.New(op, fault.Internal, fmt.Errorf("persisted capability grant is invalid: %w", err))
	}
	return grant, nil
}

func (s *Store) StageEffect(ctx context.Context, effect domain.Effect, desired domain.Artifact, previous *domain.Artifact) (domain.Effect, bool, error) {
	const op = "effect.stage"
	if err := effect.Validate(); err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, err)
	}
	if err := desired.Validate(); err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, fmt.Errorf("invalid desired artifact: %w", err))
	}
	if desired.ID != effect.DesiredArtifactID {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, errors.New("desired artifact does not match effect"))
	}
	if previous == nil && effect.PreviousExisted {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, errors.New("previous artifact is missing"))
	}
	if previous != nil {
		if err := previous.Validate(); err != nil {
			return domain.Effect{}, false, fault.New(op, fault.Invalid, fmt.Errorf("invalid previous artifact: %w", err))
		}
		if previous.ID != effect.PreviousArtifactID {
			return domain.Effect{}, false, fault.New(op, fault.Invalid, errors.New("previous artifact does not match effect"))
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()

	existing, found, err := effectByIdempotencyTx(ctx, tx, effect.RunID, effect.IdempotencyKey)
	if err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Internal, err)
	}
	if found {
		return existing, false, nil
	}

	var relationCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM runs r
		JOIN plans p ON p.id = ? AND p.contract_id = r.contract_id
		JOIN capability_grants g ON g.id = ? AND g.run_id = r.id
		WHERE r.id = ?`, effect.PlanID, effect.CapabilityGrantID, effect.RunID,
	).Scan(&relationCount); err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Internal, fmt.Errorf("validate effect relationships: %w", err))
	}
	if relationCount != 1 {
		return domain.Effect{}, false, fault.New(op, fault.Invalid, errors.New("effect run, plan, and capability grant do not share one contract"))
	}
	if err := putArtifact(ctx, tx, desired); err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Internal, err)
	}
	if previous != nil {
		if err := putArtifact(ctx, tx, *previous); err != nil {
			return domain.Effect{}, false, fault.New(op, fault.Internal, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO effects (
			id, run_id, plan_id, step_id, capability_grant_id, idempotency_key, class, status,
			target, desired_artifact_id, previous_artifact_id, previous_existed,
			created_at_unix_nano, updated_at_unix_nano
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)`,
		effect.ID, effect.RunID, effect.PlanID, effect.StepID, effect.CapabilityGrantID,
		effect.IdempotencyKey, effect.Class, effect.Status, effect.Target, effect.DesiredArtifactID,
		effect.PreviousArtifactID, effect.PreviousExisted, effect.CreatedAt.UnixNano(), effect.UpdatedAt.UnixNano(),
	); err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Conflict, fmt.Errorf("insert effect: %w", err))
	}
	if err := insertEvent(ctx, tx, "Effect", effect.ID, domain.EventEffectStaged, effect.CreatedAt, effect); err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Effect{}, false, fault.New(op, fault.Internal, fmt.Errorf("commit effect and event: %w", err))
	}
	return effect, true, nil
}

func (s *Store) Effect(ctx context.Context, id string) (domain.Effect, error) {
	effect, found, err := effectQuery(ctx, s.db.QueryRowContext, `WHERE id = ?`, id)
	if err != nil {
		return domain.Effect{}, fault.New("effect.get", fault.Internal, err)
	}
	if !found {
		return domain.Effect{}, fault.New("effect.get", fault.NotFound, fmt.Errorf("effect %q does not exist", id))
	}
	return effect, nil
}

func (s *Store) EffectByIdempotency(ctx context.Context, runID, key string) (domain.Effect, error) {
	effect, found, err := effectQuery(ctx, s.db.QueryRowContext, `WHERE run_id = ? AND idempotency_key = ?`, runID, key)
	if err != nil {
		return domain.Effect{}, fault.New("effect.idempotency", fault.Internal, err)
	}
	if !found {
		return domain.Effect{}, fault.New("effect.idempotency", fault.NotFound, fmt.Errorf("effect for run %q and key %q does not exist", runID, key))
	}
	return effect, nil
}

func (s *Store) EffectsByStatus(ctx context.Context, status domain.EffectStatus) ([]domain.Effect, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, plan_id, step_id, capability_grant_id, idempotency_key, class, status,
		       target, desired_artifact_id, COALESCE(previous_artifact_id, ''), previous_existed,
		       created_at_unix_nano, updated_at_unix_nano
		FROM effects WHERE status = ? ORDER BY created_at_unix_nano, id`, status)
	if err != nil {
		return nil, fault.New("effect.list_status", fault.Internal, fmt.Errorf("query effects: %w", err))
	}
	defer rows.Close()
	result := make([]domain.Effect, 0)
	for rows.Next() {
		effect, found, err := scanEffect(rows)
		if err != nil {
			return nil, fault.New("effect.list_status", fault.Internal, err)
		}
		if found {
			result = append(result, effect)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fault.New("effect.list_status", fault.Internal, fmt.Errorf("iterate effects: %w", err))
	}
	return result, nil
}

func (s *Store) Artifact(ctx context.Context, id string) (domain.Artifact, error) {
	var artifact domain.Artifact
	err := s.db.QueryRowContext(ctx,
		`SELECT id, algorithm, digest, media_type, size, data FROM artifacts WHERE id = ?`, id,
	).Scan(&artifact.ID, &artifact.Algorithm, &artifact.Digest, &artifact.MediaType, &artifact.Size, &artifact.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Artifact{}, fault.New("artifact.get", fault.NotFound, fmt.Errorf("artifact %q does not exist", id))
	}
	if err != nil {
		return domain.Artifact{}, fault.New("artifact.get", fault.Internal, fmt.Errorf("read artifact: %w", err))
	}
	if err := artifact.Validate(); err != nil {
		return domain.Artifact{}, fault.New("artifact.get", fault.Internal, fmt.Errorf("persisted artifact is invalid: %w", err))
	}
	return artifact, nil
}

func (s *Store) BeginEffectExecution(ctx context.Context, effectID string, now time.Time) (domain.Effect, error) {
	const op = "effect.begin_execution"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()

	effect, found, err := effectByIDTx(ctx, tx, effectID)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if !found {
		return domain.Effect{}, fault.New(op, fault.NotFound, fmt.Errorf("effect %q does not exist", effectID))
	}
	if effect.Status == domain.EffectExecuting || effect.Status == domain.EffectExecuted || effect.Status == domain.EffectCommitted {
		return effect, nil
	}
	if effect.Status != domain.EffectStaged {
		return domain.Effect{}, fault.New(op, fault.Conflict, fmt.Errorf("effect %q cannot execute from %s", effectID, effect.Status))
	}
	effect.Status = domain.EffectExecuting
	effect.UpdatedAt = now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE effects SET status = ?, updated_at_unix_nano = ? WHERE id = ? AND status = ?`,
		effect.Status, effect.UpdatedAt.UnixNano(), effect.ID, domain.EffectStaged)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("transition effect: %w", err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("count transitioned effects: %w", err))
	}
	if affected != 1 {
		return domain.Effect{}, fault.New(op, fault.Conflict, fmt.Errorf("stale effect transition affected %d rows", affected))
	}
	if err := insertEvent(ctx, tx, "Effect", effect.ID, domain.EventEffectExecutionStarted, effect.UpdatedAt, effect); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("commit execution start: %w", err))
	}
	return effect, nil
}

func (s *Store) CompleteEffectExecution(ctx context.Context, effectID string, observed domain.Artifact, evidence domain.Evidence, now time.Time) (domain.Effect, error) {
	const op = "effect.complete_execution"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()
	effect, found, err := effectByIDTx(ctx, tx, effectID)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if !found {
		return domain.Effect{}, fault.New(op, fault.NotFound, fmt.Errorf("effect %q does not exist", effectID))
	}
	if effect.Status != domain.EffectExecuting {
		return domain.Effect{}, fault.New(op, fault.Conflict, fmt.Errorf("effect %q cannot complete execution from %s", effectID, effect.Status))
	}
	if observed.ID != effect.DesiredArtifactID || evidence.EffectID != effect.ID || evidence.ArtifactID != observed.ID {
		return domain.Effect{}, fault.New(op, fault.Invalid, errors.New("execution evidence does not prove the desired artifact"))
	}
	if err := putArtifact(ctx, tx, observed); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if err := insertEvidence(ctx, tx, evidence); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	effect.Status = domain.EffectExecuted
	effect.UpdatedAt = now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE effects SET status = ?, updated_at_unix_nano = ? WHERE id = ? AND status = ?`,
		effect.Status, effect.UpdatedAt.UnixNano(), effect.ID, domain.EffectExecuting)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("transition effect: %w", err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("count transitioned effects: %w", err))
	}
	if affected != 1 {
		return domain.Effect{}, fault.New(op, fault.Conflict, fmt.Errorf("stale effect transition affected %d rows", affected))
	}
	if err := insertEvent(ctx, tx, "Effect", effect.ID, domain.EventEffectExecuted, effect.UpdatedAt, effect); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if err := insertEvent(ctx, tx, "Evidence", evidence.ID, domain.EventEvidenceRecorded, evidence.ObservedAt, evidence); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("commit execution evidence: %w", err))
	}
	return effect, nil
}

func (s *Store) MarkEffectAmbiguous(ctx context.Context, effectID, reason string, now time.Time) error {
	const op = "effect.mark_ambiguous"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE effects SET status = ?, updated_at_unix_nano = ? WHERE id = ? AND status = ?`,
		domain.EffectAmbiguous, now.UTC().UnixNano(), effectID, domain.EffectExecuting)
	if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("transition effect: %w", err))
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fault.New(op, fault.Conflict, fmt.Errorf("effect %q is not executing", effectID))
	}
	payload := struct {
		EffectID string `json:"effect_id"`
		Reason   string `json:"reason"`
	}{EffectID: effectID, Reason: reason}
	if err := insertEvent(ctx, tx, "Effect", effectID, domain.EventEffectAmbiguous, now.UTC(), payload); err != nil {
		return fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("commit ambiguous effect: %w", err))
	}
	return nil
}

func (s *Store) RecordVerification(ctx context.Context, effectID string, observed domain.Artifact, evidence domain.Evidence, verification domain.Verification, now time.Time) (domain.Effect, error) {
	const op = "verification.record"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()
	effect, found, err := effectByIDTx(ctx, tx, effectID)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if !found {
		return domain.Effect{}, fault.New(op, fault.NotFound, fmt.Errorf("effect %q does not exist", effectID))
	}
	if effect.Status != domain.EffectExecuted {
		return domain.Effect{}, fault.New(op, fault.Conflict, fmt.Errorf("effect %q cannot be verified from %s", effectID, effect.Status))
	}
	if evidence.EffectID != effect.ID || evidence.ArtifactID != observed.ID || verification.EffectID != effect.ID || verification.EvidenceID != evidence.ID {
		return domain.Effect{}, fault.New(op, fault.Invalid, errors.New("verification references do not agree"))
	}
	if verification.Status == domain.VerificationPassed && observed.ID != effect.DesiredArtifactID {
		return domain.Effect{}, fault.New(op, fault.Invalid, errors.New("passing verification does not reference desired artifact"))
	}
	if verification.Status == domain.VerificationFailed && observed.ID == effect.DesiredArtifactID {
		return domain.Effect{}, fault.New(op, fault.Invalid, errors.New("failed verification unexpectedly references desired artifact"))
	}
	if err := putArtifact(ctx, tx, observed); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if err := insertEvidence(ctx, tx, evidence); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO verifications (id, effect_id, evidence_id, method, status, observed_at_unix_nano)
		VALUES (?, ?, ?, ?, ?, ?)`, verification.ID, verification.EffectID, verification.EvidenceID,
		verification.Method, verification.Status, verification.ObservedAt.UnixNano()); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("insert verification: %w", err))
	}
	if verification.Status == domain.VerificationPassed {
		effect.Status = domain.EffectCommitted
	} else {
		effect.Status = domain.EffectFailed
	}
	effect.UpdatedAt = now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE effects SET status = ?, updated_at_unix_nano = ? WHERE id = ? AND status = ?`,
		effect.Status, effect.UpdatedAt.UnixNano(), effect.ID, domain.EffectExecuted)
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("transition verified effect: %w", err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("count transitioned effects: %w", err))
	}
	if affected != 1 {
		return domain.Effect{}, fault.New(op, fault.Conflict, fmt.Errorf("stale effect transition affected %d rows", affected))
	}
	verificationEvent := domain.EventVerificationFailed
	if verification.Status == domain.VerificationPassed {
		verificationEvent = domain.EventVerificationPassed
	}
	if err := insertEvent(ctx, tx, "Evidence", evidence.ID, domain.EventEvidenceRecorded, evidence.ObservedAt, evidence); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if err := insertEvent(ctx, tx, "Verification", verification.ID, verificationEvent, verification.ObservedAt, verification); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, err)
	}
	if verification.Status == domain.VerificationPassed {
		if err := insertEvent(ctx, tx, "Effect", effect.ID, domain.EventEffectCommitted, effect.UpdatedAt, effect); err != nil {
			return domain.Effect{}, fault.New(op, fault.Internal, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Effect{}, fault.New(op, fault.Internal, fmt.Errorf("commit verification: %w", err))
	}
	return effect, nil
}

func (s *Store) EvidenceForEffect(ctx context.Context, effectID string) ([]domain.Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, effect_id, artifact_id, kind, source, observed_at_unix_nano
		FROM evidence WHERE effect_id = ? ORDER BY observed_at_unix_nano, id`, effectID)
	if err != nil {
		return nil, fault.New("evidence.list", fault.Internal, fmt.Errorf("query evidence: %w", err))
	}
	defer rows.Close()
	result := make([]domain.Evidence, 0)
	for rows.Next() {
		var evidence domain.Evidence
		var observedAt int64
		if err := rows.Scan(&evidence.ID, &evidence.EffectID, &evidence.ArtifactID, &evidence.Kind, &evidence.Source, &observedAt); err != nil {
			return nil, fault.New("evidence.list", fault.Internal, fmt.Errorf("scan evidence: %w", err))
		}
		evidence.ObservedAt = time.Unix(0, observedAt).UTC()
		result = append(result, evidence)
	}
	return result, rows.Err()
}

func (s *Store) VerificationsForEffect(ctx context.Context, effectID string) ([]domain.Verification, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, effect_id, evidence_id, method, status, observed_at_unix_nano
		FROM verifications WHERE effect_id = ? ORDER BY observed_at_unix_nano, id`, effectID)
	if err != nil {
		return nil, fault.New("verification.list", fault.Internal, fmt.Errorf("query verifications: %w", err))
	}
	defer rows.Close()
	result := make([]domain.Verification, 0)
	for rows.Next() {
		var verification domain.Verification
		var observedAt int64
		if err := rows.Scan(&verification.ID, &verification.EffectID, &verification.EvidenceID, &verification.Method, &verification.Status, &observedAt); err != nil {
			return nil, fault.New("verification.list", fault.Internal, fmt.Errorf("scan verification: %w", err))
		}
		verification.ObservedAt = time.Unix(0, observedAt).UTC()
		result = append(result, verification)
	}
	return result, rows.Err()
}

type queryRower func(context.Context, string, ...any) *sql.Row

type rowScanner interface {
	Scan(...any) error
}

func effectQuery(ctx context.Context, queryRow queryRower, where string, args ...any) (domain.Effect, bool, error) {
	query := `SELECT id, run_id, plan_id, step_id, capability_grant_id, idempotency_key, class, status,
	                 target, desired_artifact_id, COALESCE(previous_artifact_id, ''), previous_existed,
	                 created_at_unix_nano, updated_at_unix_nano
	          FROM effects ` + where
	return scanEffect(queryRow(ctx, query, args...))
}

func scanEffect(scanner rowScanner) (domain.Effect, bool, error) {
	var effect domain.Effect
	var previousExisted int
	var createdAt, updatedAt int64
	err := scanner.Scan(
		&effect.ID, &effect.RunID, &effect.PlanID, &effect.StepID, &effect.CapabilityGrantID,
		&effect.IdempotencyKey, &effect.Class, &effect.Status, &effect.Target,
		&effect.DesiredArtifactID, &effect.PreviousArtifactID, &previousExisted, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Effect{}, false, nil
	}
	if err != nil {
		return domain.Effect{}, false, fmt.Errorf("read effect: %w", err)
	}
	effect.PreviousExisted = previousExisted == 1
	effect.CreatedAt = time.Unix(0, createdAt).UTC()
	effect.UpdatedAt = time.Unix(0, updatedAt).UTC()
	if err := effect.Validate(); err != nil {
		return domain.Effect{}, false, fmt.Errorf("persisted effect is invalid: %w", err)
	}
	return effect, true, nil
}

func effectByIDTx(ctx context.Context, tx *sql.Tx, id string) (domain.Effect, bool, error) {
	return effectQuery(ctx, tx.QueryRowContext, `WHERE id = ?`, id)
}

func effectByIdempotencyTx(ctx context.Context, tx *sql.Tx, runID, key string) (domain.Effect, bool, error) {
	return effectQuery(ctx, tx.QueryRowContext, `WHERE run_id = ? AND idempotency_key = ?`, runID, key)
}

func putArtifact(ctx context.Context, tx *sql.Tx, artifact domain.Artifact) error {
	if err := artifactutil.Verify(artifact); err != nil {
		return fmt.Errorf("invalid artifact: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifacts (id, algorithm, digest, media_type, size, data)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`, artifact.ID, artifact.Algorithm, artifact.Digest,
		artifact.MediaType, artifact.Size, artifact.Data); err != nil {
		return fmt.Errorf("insert artifact: %w", err)
	}
	var existing domain.Artifact
	if err := tx.QueryRowContext(ctx,
		`SELECT id, algorithm, digest, media_type, size, data FROM artifacts WHERE id = ?`, artifact.ID,
	).Scan(&existing.ID, &existing.Algorithm, &existing.Digest, &existing.MediaType, &existing.Size, &existing.Data); err != nil {
		return fmt.Errorf("read artifact after insert: %w", err)
	}
	if existing.Algorithm != artifact.Algorithm || existing.Digest != artifact.Digest ||
		existing.Size != artifact.Size || !bytes.Equal(existing.Data, artifact.Data) {
		return fmt.Errorf("artifact id %q has conflicting content", artifact.ID)
	}
	return nil
}

func insertEvidence(ctx context.Context, tx *sql.Tx, evidence domain.Evidence) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO evidence (id, effect_id, artifact_id, kind, source, observed_at_unix_nano)
		VALUES (?, ?, ?, ?, ?, ?)`, evidence.ID, evidence.EffectID, evidence.ArtifactID,
		evidence.Kind, evidence.Source, evidence.ObservedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert evidence: %w", err)
	}
	return nil
}
