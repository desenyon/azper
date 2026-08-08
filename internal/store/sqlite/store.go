package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"azper/internal/domain"
	"azper/internal/fault"
	"azper/internal/identity"

	_ "modernc.org/sqlite"
)

const schemaVersion = 4

type migration struct {
	version    int
	statements []string
}

var schemaMigrations = []migration{
	{
		version: 1,
		statements: []string{
			`CREATE TABLE contracts (
				id TEXT PRIMARY KEY,
				objective TEXT NOT NULL,
				success_conditions_json TEXT NOT NULL,
				created_at_unix_nano INTEGER NOT NULL
			) STRICT`,
			`CREATE TABLE runs (
				id TEXT PRIMARY KEY,
				contract_id TEXT NOT NULL REFERENCES contracts(id),
				status TEXT NOT NULL CHECK (status IN ('Running', 'Completed', 'PartiallyCompleted', 'Blocked', 'Failed', 'Cancelled', 'Superseded')),
				created_at_unix_nano INTEGER NOT NULL,
				updated_at_unix_nano INTEGER NOT NULL
			) STRICT`,
			`CREATE INDEX runs_contract_id_idx ON runs(contract_id)`,
			`CREATE TABLE events (
				sequence INTEGER PRIMARY KEY AUTOINCREMENT,
				id TEXT NOT NULL UNIQUE,
				aggregate_type TEXT NOT NULL,
				aggregate_id TEXT NOT NULL,
				type TEXT NOT NULL,
				occurred_at_unix_nano INTEGER NOT NULL,
				payload_json TEXT NOT NULL
			) STRICT`,
			`CREATE INDEX events_aggregate_idx ON events(aggregate_id, sequence)`,
		},
	},
	{
		version: 2,
		statements: []string{
			`CREATE TABLE plans (
				id TEXT PRIMARY KEY,
				contract_id TEXT NOT NULL REFERENCES contracts(id),
				plan_json TEXT NOT NULL,
				created_at_unix_nano INTEGER NOT NULL
			) STRICT`,
			`CREATE INDEX plans_contract_id_idx ON plans(contract_id)`,
		},
	},
	{
		version: 3,
		statements: []string{
			`CREATE TABLE capability_grants (
				id TEXT PRIMARY KEY,
				run_id TEXT NOT NULL REFERENCES runs(id),
				worker_id TEXT NOT NULL,
				capability TEXT NOT NULL,
				scope TEXT NOT NULL,
				effect_class TEXT NOT NULL,
				approval_source TEXT NOT NULL,
				granted_at_unix_nano INTEGER NOT NULL,
				expires_at_unix_nano INTEGER NOT NULL
			) STRICT`,
			`CREATE INDEX capability_grants_run_id_idx ON capability_grants(run_id)`,
			`CREATE TABLE artifacts (
				id TEXT PRIMARY KEY,
				algorithm TEXT NOT NULL,
				digest TEXT NOT NULL,
				media_type TEXT NOT NULL,
				size INTEGER NOT NULL,
				data BLOB NOT NULL
			) STRICT`,
			`CREATE TABLE effects (
				id TEXT PRIMARY KEY,
				run_id TEXT NOT NULL REFERENCES runs(id),
				plan_id TEXT NOT NULL REFERENCES plans(id),
				step_id TEXT NOT NULL,
				capability_grant_id TEXT NOT NULL REFERENCES capability_grants(id),
				idempotency_key TEXT NOT NULL,
				class TEXT NOT NULL,
				status TEXT NOT NULL CHECK (status IN ('Staged', 'Executing', 'Executed', 'Committed', 'Ambiguous', 'Failed')),
				target TEXT NOT NULL,
				desired_artifact_id TEXT NOT NULL REFERENCES artifacts(id),
				previous_artifact_id TEXT REFERENCES artifacts(id),
				previous_existed INTEGER NOT NULL CHECK (previous_existed IN (0, 1)),
				created_at_unix_nano INTEGER NOT NULL,
				updated_at_unix_nano INTEGER NOT NULL,
				UNIQUE (run_id, idempotency_key)
			) STRICT`,
			`CREATE INDEX effects_run_id_idx ON effects(run_id)`,
			`CREATE TABLE evidence (
				id TEXT PRIMARY KEY,
				effect_id TEXT NOT NULL REFERENCES effects(id),
				artifact_id TEXT NOT NULL REFERENCES artifacts(id),
				kind TEXT NOT NULL,
				source TEXT NOT NULL,
				observed_at_unix_nano INTEGER NOT NULL
			) STRICT`,
			`CREATE INDEX evidence_effect_id_idx ON evidence(effect_id)`,
			`CREATE TABLE verifications (
				id TEXT PRIMARY KEY,
				effect_id TEXT NOT NULL REFERENCES effects(id),
				evidence_id TEXT NOT NULL REFERENCES evidence(id),
				method TEXT NOT NULL,
				status TEXT NOT NULL CHECK (status IN ('Passed', 'Failed')),
				observed_at_unix_nano INTEGER NOT NULL
			) STRICT`,
			`CREATE INDEX verifications_effect_id_idx ON verifications(effect_id)`,
		},
	},
	{
		version: 4,
		statements: []string{
			`CREATE TABLE compensations (
				id TEXT PRIMARY KEY,
				effect_id TEXT NOT NULL UNIQUE REFERENCES effects(id),
				capability_grant_id TEXT NOT NULL REFERENCES capability_grants(id),
				status TEXT NOT NULL CHECK (status IN ('Staged', 'Executing', 'Executed', 'Compensated', 'Ambiguous', 'Failed')),
				target TEXT NOT NULL,
				restore_artifact_id TEXT REFERENCES artifacts(id),
				remove_target INTEGER NOT NULL CHECK (remove_target IN (0, 1)),
				created_at_unix_nano INTEGER NOT NULL,
				updated_at_unix_nano INTEGER NOT NULL,
				CHECK ((remove_target = 1 AND restore_artifact_id IS NULL) OR (remove_target = 0 AND restore_artifact_id IS NOT NULL))
			) STRICT`,
			`CREATE INDEX compensations_status_idx ON compensations(status)`,
			`CREATE TABLE compensation_evidence (
				id TEXT PRIMARY KEY,
				compensation_id TEXT NOT NULL REFERENCES compensations(id),
				artifact_id TEXT NOT NULL REFERENCES artifacts(id),
				kind TEXT NOT NULL,
				source TEXT NOT NULL,
				observed_at_unix_nano INTEGER NOT NULL
			) STRICT`,
			`CREATE INDEX compensation_evidence_idx ON compensation_evidence(compensation_id)`,
			`CREATE TABLE compensation_verifications (
				id TEXT PRIMARY KEY,
				compensation_id TEXT NOT NULL REFERENCES compensations(id),
				evidence_id TEXT NOT NULL REFERENCES compensation_evidence(id),
				method TEXT NOT NULL,
				status TEXT NOT NULL CHECK (status IN ('Passed', 'Failed')),
				observed_at_unix_nano INTEGER NOT NULL
			) STRICT`,
			`CREATE INDEX compensation_verifications_idx ON compensation_verifications(compensation_id)`,
		},
	},
}

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	const op = "sqlite.open"
	if path == "" {
		return nil, fault.New(op, fault.Invalid, errors.New("database path is required"))
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fault.New(op, fault.Invalid, fmt.Errorf("resolve database path: %w", err))
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, fault.New(op, fault.Internal, fmt.Errorf("create database directory: %w", err))
	}

	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	dsn := (&url.URL{Scheme: "file", Path: absPath, RawQuery: query.Encode()}).String()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fault.New(op, fault.Internal, fmt.Errorf("open database: %w", err))
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fault.New(op, fault.Internal, fmt.Errorf("ping database: %w", err))
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	const op = "sqlite.migrate"

	var journalMode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("enable WAL: %w", err))
	}

	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("read schema version: %w", err))
	}
	if current > schemaVersion {
		return fault.New(op, fault.Conflict, fmt.Errorf("database schema version %d is newer than supported version %d", current, schemaVersion))
	}
	for current < schemaVersion {
		next := current + 1
		if next > len(schemaMigrations) || schemaMigrations[next-1].version != next {
			return fault.New(op, fault.Internal, fmt.Errorf("missing schema migration %d", next))
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fault.New(op, fault.Internal, fmt.Errorf("begin migration %d: %w", next, err))
		}
		for _, statement := range schemaMigrations[next-1].statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return fault.New(op, fault.Internal, fmt.Errorf("apply schema version %d: %w", next, err))
			}
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", next)); err != nil {
			_ = tx.Rollback()
			return fault.New(op, fault.Internal, fmt.Errorf("record schema version %d: %w", next, err))
		}
		if err := tx.Commit(); err != nil {
			return fault.New(op, fault.Internal, fmt.Errorf("commit schema version %d: %w", next, err))
		}
		current = next
	}
	return nil
}

func (s *Store) CreateContract(ctx context.Context, contract domain.Contract) error {
	const op = "contract.create"
	if err := contract.Validate(); err != nil {
		return fault.New(op, fault.Invalid, err)
	}
	conditions, err := json.Marshal(contract.SuccessConditions)
	if err != nil {
		return fault.New(op, fault.Invalid, fmt.Errorf("encode success conditions: %w", err))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO contracts (id, objective, success_conditions_json, created_at_unix_nano) VALUES (?, ?, ?, ?)`,
		contract.ID, contract.Objective, string(conditions), contract.CreatedAt.UnixNano(),
	); err != nil {
		return fault.New(op, fault.Conflict, fmt.Errorf("insert contract: %w", err))
	}
	if err := insertEvent(ctx, tx, "Contract", contract.ID, domain.EventContractCreated, contract.CreatedAt, contract); err != nil {
		return fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("commit contract and event: %w", err))
	}
	return nil
}

func (s *Store) Contract(ctx context.Context, id string) (domain.Contract, error) {
	const op = "contract.get"
	var contract domain.Contract
	var conditions string
	var createdAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, objective, success_conditions_json, created_at_unix_nano FROM contracts WHERE id = ?`, id,
	).Scan(&contract.ID, &contract.Objective, &conditions, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Contract{}, fault.New(op, fault.NotFound, fmt.Errorf("contract %q does not exist", id))
	}
	if err != nil {
		return domain.Contract{}, fault.New(op, fault.Internal, fmt.Errorf("read contract: %w", err))
	}
	if err := json.Unmarshal([]byte(conditions), &contract.SuccessConditions); err != nil {
		return domain.Contract{}, fault.New(op, fault.Internal, fmt.Errorf("decode success conditions: %w", err))
	}
	contract.CreatedAt = time.Unix(0, createdAt).UTC()
	return contract, nil
}

func (s *Store) StartRun(ctx context.Context, run domain.Run) error {
	const op = "run.start"
	if err := run.Validate(); err != nil {
		return fault.New(op, fault.Invalid, err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM contracts WHERE id = ?`, run.ContractID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return fault.New(op, fault.NotFound, fmt.Errorf("contract %q does not exist", run.ContractID))
	} else if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("read contract: %w", err))
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO runs (id, contract_id, status, created_at_unix_nano, updated_at_unix_nano) VALUES (?, ?, ?, ?, ?)`,
		run.ID, run.ContractID, run.Status, run.CreatedAt.UnixNano(), run.UpdatedAt.UnixNano(),
	); err != nil {
		return fault.New(op, fault.Conflict, fmt.Errorf("insert run: %w", err))
	}
	if err := insertEvent(ctx, tx, "Run", run.ID, domain.EventRunStarted, run.CreatedAt, run); err != nil {
		return fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("commit run and event: %w", err))
	}
	return nil
}

func (s *Store) Run(ctx context.Context, id string) (domain.Run, error) {
	const op = "run.get"
	var run domain.Run
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, contract_id, status, created_at_unix_nano, updated_at_unix_nano FROM runs WHERE id = ?`, id,
	).Scan(&run.ID, &run.ContractID, &run.Status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Run{}, fault.New(op, fault.NotFound, fmt.Errorf("run %q does not exist", id))
	}
	if err != nil {
		return domain.Run{}, fault.New(op, fault.Internal, fmt.Errorf("read run: %w", err))
	}
	run.CreatedAt = time.Unix(0, createdAt).UTC()
	run.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return run, nil
}

func (s *Store) CreatePlan(ctx context.Context, plan domain.Plan) error {
	const op = "plan.create"
	if err := plan.Validate(); err != nil {
		return fault.New(op, fault.Invalid, err)
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return fault.New(op, fault.Invalid, fmt.Errorf("encode plan: %w", err))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("begin transaction: %w", err))
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM contracts WHERE id = ?`, plan.ContractID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return fault.New(op, fault.NotFound, fmt.Errorf("contract %q does not exist", plan.ContractID))
	} else if err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("read contract: %w", err))
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO plans (id, contract_id, plan_json, created_at_unix_nano) VALUES (?, ?, ?, ?)`,
		plan.ID, plan.ContractID, string(planJSON), plan.CreatedAt.UnixNano(),
	); err != nil {
		return fault.New(op, fault.Conflict, fmt.Errorf("insert plan: %w", err))
	}
	if err := insertEvent(ctx, tx, "Plan", plan.ID, domain.EventPlanCreated, plan.CreatedAt, plan); err != nil {
		return fault.New(op, fault.Internal, err)
	}
	if err := tx.Commit(); err != nil {
		return fault.New(op, fault.Internal, fmt.Errorf("commit plan and event: %w", err))
	}
	return nil
}

func (s *Store) Plan(ctx context.Context, id string) (domain.Plan, error) {
	const op = "plan.get"
	var planJSON string
	var contractID string
	var createdAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT contract_id, plan_json, created_at_unix_nano FROM plans WHERE id = ?`, id,
	).Scan(&contractID, &planJSON, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, fault.New(op, fault.NotFound, fmt.Errorf("plan %q does not exist", id))
	}
	if err != nil {
		return domain.Plan{}, fault.New(op, fault.Internal, fmt.Errorf("read plan: %w", err))
	}

	var plan domain.Plan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return domain.Plan{}, fault.New(op, fault.Internal, fmt.Errorf("decode plan: %w", err))
	}
	if plan.ID != id || plan.ContractID != contractID || plan.CreatedAt.UnixNano() != createdAt {
		return domain.Plan{}, fault.New(op, fault.Internal, errors.New("plan envelope does not match persisted columns"))
	}
	if err := plan.Validate(); err != nil {
		return domain.Plan{}, fault.New(op, fault.Internal, fmt.Errorf("persisted plan is invalid: %w", err))
	}
	return plan, nil
}

func (s *Store) Events(ctx context.Context, aggregateID string) ([]domain.Event, error) {
	const op = "event.list"
	query := `SELECT sequence, id, aggregate_type, aggregate_id, type, occurred_at_unix_nano, payload_json FROM events`
	args := []any{}
	if aggregateID != "" {
		query += ` WHERE aggregate_id = ?`
		args = append(args, aggregateID)
	}
	query += ` ORDER BY sequence`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fault.New(op, fault.Internal, fmt.Errorf("query events: %w", err))
	}
	defer rows.Close()

	events := make([]domain.Event, 0)
	for rows.Next() {
		var event domain.Event
		var occurredAt int64
		var payload string
		if err := rows.Scan(&event.Sequence, &event.ID, &event.AggregateType, &event.AggregateID, &event.Type, &occurredAt, &payload); err != nil {
			return nil, fault.New(op, fault.Internal, fmt.Errorf("scan event: %w", err))
		}
		event.OccurredAt = time.Unix(0, occurredAt).UTC()
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fault.New(op, fault.Internal, fmt.Errorf("iterate events: %w", err))
	}
	return events, nil
}

func insertEvent(ctx context.Context, tx *sql.Tx, aggregateType, aggregateID, eventType string, occurredAt time.Time, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s payload: %w", eventType, err)
	}
	eventID, err := identity.New("evt", occurredAt)
	if err != nil {
		return fmt.Errorf("create %s event identifier: %w", eventType, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events (id, aggregate_type, aggregate_id, type, occurred_at_unix_nano, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
		eventID, aggregateType, aggregateID, eventType, occurredAt.UnixNano(), string(payloadJSON),
	); err != nil {
		return fmt.Errorf("insert %s event: %w", eventType, err)
	}
	return nil
}
