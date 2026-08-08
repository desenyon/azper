# Roadmap

Updated: 2026-08-08

## Current milestone

Let Contract semantics—not tool success—control Run completion, building on the recovered and compensated first governed Effect.

## What works now

- Typed Contract and Run creation.
- Versioned SQLite schema.
- Atomic aggregate and event persistence.
- Typed PlanIR with deterministic DAG, observability, Effect, capability, risk, and compensation validation.
- Atomic Plan persistence and a tested schema-v1-to-v2 upgrade.
- Restart-safe local inspection through the CLI.
- Failure, cancellation, duplicate, race, and CLI tests for the implemented path.
- A Run-bound, scope-bound, expiring capability grant for one local file Effect.
- Durable Effect staging, BLAKE3 artifacts, idempotent execution, ambiguous-outcome reconciliation, independent Evidence, Verification, and Commit.
- Real CLI execution through `azper file write`.
- Bounded manual recovery through `azper recover`, including restart reconciliation and explicit needs-attention outcomes.
- Durable, idempotent file Compensation through `azper undo`, including restore, removal, drift refusal, independent Verification, and restart recovery.

## Critical missing pieces

- No planner selects a Milestone or derives an executable frontier from PlanIR.
- Recovery is explicit through the CLI; daemon startup does not invoke it automatically.
- Effect Verification is not yet mapped to Step postconditions or Contract success conditions, so Runs cannot truthfully complete.
- Capability approval exists only as an explicit CLI command; there is no approval UX or grant renewal flow.
- File Artifacts do not independently capture or verify permission mode.
- Later drift is detected on terminal-state retry, but durable Verification invalidation and supersession are not modeled yet.

## Highest-leverage next slices

1. Map verified Evidence to Step postconditions, select the first executable frontier, and let the Verifier evaluate Contract success before Run completion.
2. Invoke the verified recovery scan from the future daemon startup path and add leases before concurrent Workers exist.
3. Add explicit capability renewal for safe resumption of staged work without silently broadening authority.
4. Expose the verified slice through a quiet basic Bubble Tea v2 TUI shell with truthful approval and verification states.
5. Turn the slice into the first AzperBench case measuring verified completion, false success, recovery, and rollback.

## Known blockers

None for the next local slices. External providers and credentials are not required.

## Recently completed

- 2026-08-08: Bootstrapped the Go module and verified durable Contract, Run, and event-log creation through SQLite and the CLI.
- 2026-08-08: Added validated, durable PlanIR and verified the first forward schema migration.
- 2026-08-08: Added the first governed local file Effect with scoped capability, BLAKE3 artifacts, idempotent reconciliation, independent Verification, and durable Commit.
- 2026-08-08: Added bounded restart recovery for durable `Executing` file Effects with explicit committed and needs-attention outcomes.
- 2026-08-08: Added durable file Compensation and `azper undo` with restore/removal, independent Verification, drift refusal, idempotency, and restart recovery.
