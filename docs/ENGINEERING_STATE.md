# Engineering State

Updated: 2026-08-08

## Verified end to end

- A local CLI creates a typed Contract with an objective and required success conditions.
- SQLite persists Contracts, Runs, execution objects, and an ordered event log using schema version 4.
- Contract creation plus `ContractCreated`, and Run creation plus `RunStarted`, commit atomically.
- State survives store closure and process restart.
- Missing parent Contracts, duplicate Contracts, cancelled writes, and forced event-write failures do not produce false durable state.
- Errors preserve an operation and category; cancellation remains distinguishable.
- PlanIR rejects cyclic or dangling dependencies, unverifiable Steps, state mutations without scoped capability requirements, and reversible writes without compensation strategies.
- Validated Plans persist atomically with `PlanCreated`, including through a tested schema-v1-to-v2 upgrade.
- One local file write follows Observe, Stage, capability validation, durable `Executing`, atomic replacement, executor read-back Evidence, independent Verifier read-back, and Effect Commit.
- File artifacts use BLAKE3-256 content addresses; previous bytes are staged for future compensation.
- Idempotent retries return the original Effect, changed inputs under the same key are rejected, and scope or symlink escapes are rejected.
- A forced Evidence-persistence failure after the external write leaves the Effect `Executing`; retry reconciles the desired hash without a duplicate write.
- Target drift between execution and verification records a failed Verification and does not commit the Effect.
- `azper recover` performs a bounded scan of durable `Executing` Effects, reconciles and verifies safe work, and reports expired or ambiguous Effects without overwriting drift.
- `azper undo` creates one durable Compensation per committed file Effect, restores staged previous bytes or removes a newly created file, and requires independent Verification before `Compensated`.
- Compensation refuses to overwrite post-effect drift, preserves the original Effect as immutable history, and is idempotent across retries.
- Idempotent retries of terminal Effects and Compensations re-read current filesystem state instead of presenting historical passing Verification after later drift.
- A forced Compensation Evidence-persistence failure after filesystem mutation remains `Executing`; retry and restart recovery reconcile the observed state without repeating the mutation.
- Recovery scans both durable Effects and Compensations and distinguishes committed, compensated, and needs-attention outcomes.

Verification commands:

```sh
go test ./...
go test -race ./...
go vet ./...
```

## Partially implemented

- Contract: objective and success conditions are typed; the broader constitutional fields are not yet represented.
- Run: durable creation and terminal status vocabulary exist; lifecycle transitions are not yet implemented.
- PlanIR: immutable Milestone and Step graphs are typed and validated; executable-frontier selection, dynamic precondition evaluation, and replanning do not exist.
- Event log: append and aggregate inspection work; subscriptions, checkpoints, and recovery replay do not.
- CLI: expert foundation commands work; the first-class TUI does not exist.
- Effects: one local regular-file replacement and its explicit Compensation are implemented; general tool manifests, grouped approvals, and other Effect classes are not.
- Verification: file bytes are independently verified, but Contract success conditions are not evaluated and Runs cannot complete.

## Absent

Planner, scheduler, general Workers, Attempts, Contract-level Verification, Run Commit, automatic startup recovery, World Engine, memory classes, Context Compiler, Model Fabric, local inference, Hermes compatibility, MCP, ACP, skills, learning, automations, API daemon, TUI, and AzperBench.

## Architectural debt and uncertainty

- The Go module path is the local name `azper`; a public import path has not been chosen.
- Forward migration from schema version 1 through 4 is verified. Downgrade policy and more complex data migrations remain undefined.
- SQLite uses a single open connection to make pragma and transaction behavior deterministic. Concurrency policy has not yet been benchmarked.
- Direct dependency licenses are recorded in `THIRD_PARTY_NOTICES`; release packaging still needs automated transitive license collection.
- File replacement and directory sync are verified on macOS; Windows replacement semantics have not been exercised.
- File mode is preserved in the normal replacement and restore path, but mode is not independently recorded or verified as part of the Artifact.
- An `Executing` Compensation whose grant expires before any mutation needs a future explicit grant-renewal flow; recovery will not silently broaden authority.
- A terminal-state recheck detects later drift but does not yet persist a durable invalidation Evidence record or supersede the historical Verification.
- Scope checks reject present symlink escapes and are repeated before execution, but filesystem path authorization is not yet hardened with directory file descriptors against adversarial time-of-check/time-of-use swaps.

## Active experiments

None. No experimental runtime behavior is enabled.
