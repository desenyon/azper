package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"azper/internal/domain"
	"azper/internal/fault"
)

var absentFileObservation = []byte(`{"exists":false}`)

func (s *Store) StageCompensation(ctx context.Context, compensation domain.Compensation) (domain.Compensation, bool, error) {
	const op = "compensation.stage"
	if err := compensation.Validate(); err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Invalid, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()

	existing, found, err := compensationByEffectIDTx(ctx, tx, compensation.EffectID)
	if err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Internal, err)
	}
	if found {
		return existing, false, nil
	}
	effect, found, err := effectByIDTx(ctx, tx, compensation.EffectID)
	if err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Internal, err)
	}
	if !found {
		return domain.Compensation{}, false, fault.New(op, fault.NotFound, fmt.Errorf("effect %q does not exist", compensation.EffectID))
	}
	if effect.Status != domain.EffectCommitted {
		return domain.Compensation{}, false, fault.New(op, fault.Conflict, fmt.Errorf("effect %q is %s, not Committed", effect.ID, effect.Status))
	}
	if compensation.Target != effect.Target || compensation.RemoveTarget == effect.PreviousExisted || compensation.RestoreArtifactID != effect.PreviousArtifactID {
		return domain.Compensation{}, false, fault.New(op, fault.Invalid, errors.New("compensation does not reproduce the effect's staged previous state"))
	}
	var grantCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM capability_grants
		WHERE id = ? AND run_id = ? AND capability = ? AND effect_class = ?`,
		compensation.CapabilityGrantID, effect.RunID, domain.FilesystemWriteCapability, domain.EffectReversibleWrite,
	).Scan(&grantCount); err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Internal, fmt.Errorf("validate capability grant: %w", err))
	}
	if grantCount != 1 {
		return domain.Compensation{}, false, fault.New(op, fault.Invalid, errors.New("capability grant does not authorize the effect's run"))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO compensations (
			id, effect_id, capability_grant_id, status, target, restore_artifact_id,
			remove_target, created_at_unix_nano, updated_at_unix_nano
		) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)`,
		compensation.ID, compensation.EffectID, compensation.CapabilityGrantID, compensation.Status,
		compensation.Target, compensation.RestoreArtifactID, compensation.RemoveTarget,
		compensation.CreatedAt.UnixNano(), compensation.UpdatedAt.UnixNano(),
	); err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Conflict, fmt.Errorf("insert compensation: %w", err))
	}
	if err := insertEvent(ctx, tx, "Compensation", compensation.ID, domain.EventCompensationStaged, compensation.CreatedAt, compensation); err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Compensation{}, false, fault.New(op, fault.Internal, fmt.Errorf("commit compensation and event: %w", err))
	}
	return compensation, true, nil
}

func (s *Store) Compensation(ctx context.Context, id string) (domain.Compensation, error) {
	compensation, found, err := compensationQuery(ctx, s.db.QueryRowContext, `WHERE id = ?`, id)
	if err != nil {
		return domain.Compensation{}, fault.New("compensation.get", fault.Internal, err)
	}
	if !found {
		return domain.Compensation{}, fault.New("compensation.get", fault.NotFound, fmt.Errorf("compensation %q does not exist", id))
	}
	return compensation, nil
}

func (s *Store) CompensationForEffect(ctx context.Context, effectID string) (domain.Compensation, error) {
	compensation, found, err := compensationQuery(ctx, s.db.QueryRowContext, `WHERE effect_id = ?`, effectID)
	if err != nil {
		return domain.Compensation{}, fault.New("compensation.effect", fault.Internal, err)
	}
	if !found {
		return domain.Compensation{}, fault.New("compensation.effect", fault.NotFound, fmt.Errorf("effect %q has no compensation", effectID))
	}
	return compensation, nil
}

func (s *Store) CompensationsByStatus(ctx context.Context, status domain.CompensationStatus) ([]domain.Compensation, error) {
	rows, err := s.db.QueryContext(ctx, compensationSelect+` WHERE status = ? ORDER BY created_at_unix_nano, id`, status)
	if err != nil {
		return nil, fault.New("compensation.list_status", fault.Internal, fmt.Errorf("query compensations: %w", err))
	}
	defer rows.Close()
	result := make([]domain.Compensation, 0)
	for rows.Next() {
		compensation, found, err := scanCompensation(rows)
		if err != nil {
			return nil, fault.New("compensation.list_status", fault.Internal, err)
		}
		if found {
			result = append(result, compensation)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fault.New("compensation.list_status", fault.Internal, fmt.Errorf("iterate compensations: %w", err))
	}
	return result, nil
}

func (s *Store) BeginCompensationExecution(ctx context.Context, compensationID string, now time.Time) (domain.Compensation, error) {
	const op = "compensation.begin_execution"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()
	compensation, found, err := compensationByIDTx(ctx, tx, compensationID)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if !found {
		return domain.Compensation{}, fault.New(op, fault.NotFound, fmt.Errorf("compensation %q does not exist", compensationID))
	}
	if compensation.Status == domain.CompensationExecuting || compensation.Status == domain.CompensationExecuted || compensation.Status == domain.CompensationCompensated {
		return compensation, nil
	}
	if compensation.Status != domain.CompensationStaged {
		return domain.Compensation{}, fault.New(op, fault.Conflict, fmt.Errorf("compensation %q cannot execute from %s", compensationID, compensation.Status))
	}
	compensation.Status = domain.CompensationExecuting
	compensation.UpdatedAt = now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE compensations SET status = ?, updated_at_unix_nano = ? WHERE id = ? AND status = ?`,
		compensation.Status, compensation.UpdatedAt.UnixNano(), compensation.ID, domain.CompensationStaged)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("transition compensation: %w", err))
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.Compensation{}, fault.New(op, fault.Conflict, fmt.Errorf("stale compensation transition affected %d rows", affected))
	}
	if err := insertEvent(ctx, tx, "Compensation", compensation.ID, domain.EventCompensationStarted, compensation.UpdatedAt, compensation); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("commit execution start: %w", err))
	}
	return compensation, nil
}

func (s *Store) CompleteCompensationExecution(ctx context.Context, compensationID string, observed domain.Artifact, evidence domain.CompensationEvidence, now time.Time) (domain.Compensation, error) {
	const op = "compensation.complete_execution"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()
	compensation, found, err := compensationByIDTx(ctx, tx, compensationID)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if !found {
		return domain.Compensation{}, fault.New(op, fault.NotFound, fmt.Errorf("compensation %q does not exist", compensationID))
	}
	if compensation.Status != domain.CompensationExecuting {
		return domain.Compensation{}, fault.New(op, fault.Conflict, fmt.Errorf("compensation %q cannot complete from %s", compensationID, compensation.Status))
	}
	if evidence.CompensationID != compensation.ID || evidence.ArtifactID != observed.ID || !compensationObservationMatches(compensation, observed) {
		return domain.Compensation{}, fault.New(op, fault.Invalid, errors.New("execution evidence does not prove the compensated state"))
	}
	if err := putArtifact(ctx, tx, observed); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if err := insertCompensationEvidence(ctx, tx, evidence); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	compensation.Status = domain.CompensationExecuted
	compensation.UpdatedAt = now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE compensations SET status = ?, updated_at_unix_nano = ? WHERE id = ? AND status = ?`,
		compensation.Status, compensation.UpdatedAt.UnixNano(), compensation.ID, domain.CompensationExecuting)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("transition compensation: %w", err))
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.Compensation{}, fault.New(op, fault.Conflict, fmt.Errorf("stale compensation transition affected %d rows", affected))
	}
	if err := insertEvent(ctx, tx, "Compensation", compensation.ID, domain.EventCompensationExecuted, compensation.UpdatedAt, compensation); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if err := insertEvent(ctx, tx, "CompensationEvidence", evidence.ID, domain.EventCompensationEvidence, evidence.ObservedAt, evidence); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("commit compensation evidence: %w", err))
	}
	return compensation, nil
}

func (s *Store) MarkCompensationAmbiguous(ctx context.Context, compensationID, reason string, now time.Time) error {
	const op = "compensation.mark_ambiguous"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE compensations SET status = ?, updated_at_unix_nano = ? WHERE id = ? AND status = ?`,
		domain.CompensationAmbiguous, now.UTC().UnixNano(), compensationID, domain.CompensationExecuting)
	if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("transition compensation: %w", err))
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fault.New(op, fault.Conflict, fmt.Errorf("compensation %q is not executing", compensationID))
	}
	payload := struct {
		CompensationID string `json:"compensation_id"`
		Reason         string `json:"reason"`
	}{CompensationID: compensationID, Reason: reason}
	if err := insertEvent(ctx, tx, "Compensation", compensationID, domain.EventCompensationAmbiguous, now.UTC(), payload); err != nil {
		return fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("commit ambiguous compensation: %w", err))
	}
	return nil
}

func (s *Store) RecordCompensationVerification(ctx context.Context, compensationID string, observed domain.Artifact, evidence domain.CompensationEvidence, verification domain.CompensationVerification, now time.Time) (domain.Compensation, error) {
	const op = "compensation.verify"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()
	compensation, found, err := compensationByIDTx(ctx, tx, compensationID)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if !found {
		return domain.Compensation{}, fault.New(op, fault.NotFound, fmt.Errorf("compensation %q does not exist", compensationID))
	}
	if compensation.Status != domain.CompensationExecuted {
		return domain.Compensation{}, fault.New(op, fault.Conflict, fmt.Errorf("compensation %q cannot be verified from %s", compensationID, compensation.Status))
	}
	if evidence.CompensationID != compensation.ID || evidence.ArtifactID != observed.ID || verification.CompensationID != compensation.ID || verification.EvidenceID != evidence.ID {
		return domain.Compensation{}, fault.New(op, fault.Invalid, errors.New("compensation verification references do not agree"))
	}
	matches := compensationObservationMatches(compensation, observed)
	if (verification.Status == domain.VerificationPassed) != matches {
		return domain.Compensation{}, fault.New(op, fault.Invalid, errors.New("compensation verification status disagrees with observed state"))
	}
	if err := putArtifact(ctx, tx, observed); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if err := insertCompensationEvidence(ctx, tx, evidence); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO compensation_verifications (id, compensation_id, evidence_id, method, status, observed_at_unix_nano)
		VALUES (?, ?, ?, ?, ?, ?)`, verification.ID, verification.CompensationID, verification.EvidenceID,
		verification.Method, verification.Status, verification.ObservedAt.UnixNano()); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("insert compensation verification: %w", err))
	}
	compensation.Status = domain.CompensationFailed
	if verification.Status == domain.VerificationPassed {
		compensation.Status = domain.CompensationCompensated
	}
	compensation.UpdatedAt = now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE compensations SET status = ?, updated_at_unix_nano = ? WHERE id = ? AND status = ?`,
		compensation.Status, compensation.UpdatedAt.UnixNano(), compensation.ID, domain.CompensationExecuted)
	if err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("transition verified compensation: %w", err))
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.Compensation{}, fault.New(op, fault.Conflict, fmt.Errorf("stale compensation transition affected %d rows", affected))
	}
	eventType := domain.EventCompensationFailed
	if verification.Status == domain.VerificationPassed {
		eventType = domain.EventCompensationVerified
	}
	if err := insertEvent(ctx, tx, "CompensationEvidence", evidence.ID, domain.EventCompensationEvidence, evidence.ObservedAt, evidence); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if err := insertEvent(ctx, tx, "CompensationVerification", verification.ID, eventType, verification.ObservedAt, verification); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, err)
	}
	if verification.Status == domain.VerificationPassed {
		if err := insertEvent(ctx, tx, "Effect", compensation.EffectID, domain.EventEffectCompensated, compensation.UpdatedAt, compensation); err != nil {
			return domain.Compensation{}, fault.New(op, fault.Internal, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Compensation{}, fault.New(op, fault.Internal, fmt.Errorf("commit compensation verification: %w", err))
	}
	return compensation, nil
}

func (s *Store) CompensationVerifications(ctx context.Context, compensationID string) ([]domain.CompensationVerification, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, compensation_id, evidence_id, method, status, observed_at_unix_nano
		FROM compensation_verifications WHERE compensation_id = ? ORDER BY observed_at_unix_nano, id`, compensationID)
	if err != nil {
		return nil, fault.New("compensation.verifications", fault.Internal, fmt.Errorf("query compensation verifications: %w", err))
	}
	defer rows.Close()
	result := make([]domain.CompensationVerification, 0)
	for rows.Next() {
		var verification domain.CompensationVerification
		var observedAt int64
		if err := rows.Scan(&verification.ID, &verification.CompensationID, &verification.EvidenceID, &verification.Method, &verification.Status, &observedAt); err != nil {
			return nil, fault.New("compensation.verifications", fault.Internal, fmt.Errorf("scan compensation verification: %w", err))
		}
		verification.ObservedAt = time.Unix(0, observedAt).UTC()
		result = append(result, verification)
	}
	return result, rows.Err()
}

const compensationSelect = `SELECT id, effect_id, capability_grant_id, status, target,
	COALESCE(restore_artifact_id, ''), remove_target, created_at_unix_nano, updated_at_unix_nano FROM compensations`

func compensationQuery(ctx context.Context, queryRow queryRower, where string, args ...any) (domain.Compensation, bool, error) {
	return scanCompensation(queryRow(ctx, compensationSelect+" "+where, args...))
}

func scanCompensation(scanner rowScanner) (domain.Compensation, bool, error) {
	var compensation domain.Compensation
	var removeTarget int
	var createdAt, updatedAt int64
	err := scanner.Scan(&compensation.ID, &compensation.EffectID, &compensation.CapabilityGrantID,
		&compensation.Status, &compensation.Target, &compensation.RestoreArtifactID, &removeTarget, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Compensation{}, false, nil
	}
	if err != nil {
		return domain.Compensation{}, false, fmt.Errorf("read compensation: %w", err)
	}
	compensation.RemoveTarget = removeTarget == 1
	compensation.CreatedAt = time.Unix(0, createdAt).UTC()
	compensation.UpdatedAt = time.Unix(0, updatedAt).UTC()
	if err := compensation.Validate(); err != nil {
		return domain.Compensation{}, false, fmt.Errorf("persisted compensation is invalid: %w", err)
	}
	return compensation, true, nil
}

func compensationByIDTx(ctx context.Context, tx *sql.Tx, id string) (domain.Compensation, bool, error) {
	return compensationQuery(ctx, tx.QueryRowContext, `WHERE id = ?`, id)
}

func compensationByEffectIDTx(ctx context.Context, tx *sql.Tx, effectID string) (domain.Compensation, bool, error) {
	return compensationQuery(ctx, tx.QueryRowContext, `WHERE effect_id = ?`, effectID)
}

func compensationObservationMatches(compensation domain.Compensation, observed domain.Artifact) bool {
	if !compensation.RemoveTarget {
		return observed.ID == compensation.RestoreArtifactID
	}
	return observed.MediaType == "application/vnd.azper.file-observation+json" && bytes.Equal(observed.Data, absentFileObservation)
}

func insertCompensationEvidence(ctx context.Context, tx *sql.Tx, evidence domain.CompensationEvidence) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO compensation_evidence (id, compensation_id, artifact_id, kind, source, observed_at_unix_nano)
		VALUES (?, ?, ?, ?, ?, ?)`, evidence.ID, evidence.CompensationID, evidence.ArtifactID,
		evidence.Kind, evidence.Source, evidence.ObservedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert compensation evidence: %w", err)
	}
	return nil
}
