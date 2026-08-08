# Azper

Azper is a local-first adaptive execution runtime. The current implementation is its durable foundation: typed Contracts, Runs, validated PlanIR, governed file Effects, independent Verification, and durable Compensation persisted in SQLite with an attributable event log.

It is intentionally not yet an autonomous execution engine. Contract-level completion, planning, scheduling, model routing, memory, and the TUI remain future slices.

## Try the verified path

Build the CLI:

```sh
go build -o azper ./cmd/azper
```

Create a Contract in a local database:

```sh
./azper contract create \
  --db ./azper.db \
  --objective "Persist a durable execution request" \
  --success "The contract can be read after restart"
```

Copy the returned `id`, then start and inspect a Run:

```sh
./azper contract show --db ./azper.db CONTRACT_ID
./azper run start --db ./azper.db CONTRACT_ID
./azper run show --db ./azper.db RUN_ID
./azper trace --db ./azper.db RUN_ID
```

Execute one governed local file write from that Run:

```sh
mkdir -p ./azper-output
./azper file write \
  --db ./azper.db \
  --run RUN_ID \
  --scope ./azper-output \
  --path result.txt \
  --content "verified bytes" \
  --idempotency-key result-v1
```

This explicit command creates validated PlanIR, issues a 15-minute Run-bound `filesystem.write` grant scoped to `./azper-output`, stages the previous file as a content-addressed Artifact, performs an atomic replacement, records executor Evidence, and independently reads and hashes the target before the Verifier commits the Effect. Repeating the same key and inputs returns the same Effect; changing the content under that key is rejected.

Copy the committed Effect `id` and compensate it explicitly:

```sh
./azper undo --db ./azper.db EFFECT_ID
```

Undo issues a fresh scoped grant, durably stages one Compensation, and restores the previous Artifact or removes a file that the Effect created. It only mutates when the target still matches the committed Effect output. Post-effect drift is left untouched and reported as `Ambiguous`. A separate read-back must pass before the Compensation becomes `Compensated`.

After an interrupted process, reconcile all durable `Executing` Effects and Compensations owned by the CLI worker:

```sh
./azper recover --db ./azper.db
```

Recovery commits or compensates work whose desired state can be proven, safely resumes still-authorized mutations whose expected state is unchanged, and reports expired or ambiguous work under `needs_attention`.

Expected behavior:

- Contract creation returns a `ctr_...` identifier and writes one `ContractCreated` event atomically.
- Run creation returns a `run_...` identifier in `Running` state and writes one `RunStarted` event atomically.
- `show` and `trace` still return the persisted objects after another process opens the database.
- A Run cannot start for a missing Contract.
- A file Effect reports `Committed` only after independent BLAKE3-256 verification.
- An undo reports `Compensated` only after independent verification of the restored or absent target.

The Run remains `Running`: Contract-level success evaluation and Run completion are not implemented, so the file Effect cannot claim the broader Contract is complete.

## Validate

```sh
go test ./...
go test -race ./...
go vet ./...
```

See [docs/ENGINEERING_STATE.md](docs/ENGINEERING_STATE.md) for the evidence-backed subsystem status and [docs/ROADMAP.md](docs/ROADMAP.md) for the next vertical slices.
