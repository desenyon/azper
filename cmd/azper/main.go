package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"azper/internal/domain"
	"azper/internal/fault"
	"azper/internal/kernel"
	"azper/internal/store/sqlite"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	switch args[0] {
	case "contract":
		return runContract(ctx, args[1:], stdout)
	case "run":
		return runRun(ctx, args[1:], stdout)
	case "trace":
		return runTrace(ctx, args[1:], stdout)
	case "file":
		return runFile(ctx, args[1:], stdout)
	case "recover":
		return runRecover(ctx, args[1:], stdout)
	case "help", "-h", "--help":
		_, err := fmt.Fprint(stdout, usage)
		return err
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}

func runRecover(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("recover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", defaultDBPath(), "SQLite database path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return withStore(ctx, *dbPath, func(store *sqlite.Store) error {
		recovery, err := kernel.NewRecoveryEngine(store, "cli")
		if err != nil {
			return err
		}
		report, err := recovery.Recover(ctx)
		if err != nil {
			return err
		}
		return writeJSON(stdout, report)
	})
}

func runFile(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "write" {
		return errors.New("usage: azper file write [flags]")
	}
	flags := flag.NewFlagSet("file write", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", defaultDBPath(), "SQLite database path")
	runID := flags.String("run", "", "owning Run identifier")
	scope := flags.String("scope", "", "approved filesystem directory")
	target := flags.String("path", "", "target path inside scope")
	content := flags.String("content", "", "desired UTF-8 file content")
	idempotencyKey := flags.String("idempotency-key", "", "stable key for this mutation")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *runID == "" || *scope == "" || *target == "" || *idempotencyKey == "" {
		return errors.New("--run, --scope, --path, and --idempotency-key are required")
	}
	canonicalScope, err := canonicalDirectory(*scope)
	if err != nil {
		return err
	}

	return withStore(ctx, *dbPath, func(store *sqlite.Store) error {
		result, err := executeFileCommand(ctx, store, fileCommandRequest{
			runID: *runID, scope: canonicalScope, target: *target,
			content: []byte(*content), idempotencyKey: *idempotencyKey,
		})
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	})
}

type fileCommandRequest struct {
	runID, scope, target, idempotencyKey string
	content                              []byte
}

type fileCommandResult struct {
	Effect       domain.Effect       `json:"effect"`
	Verification domain.Verification `json:"verification"`
}

func executeFileCommand(ctx context.Context, store *sqlite.Store, request fileCommandRequest) (fileCommandResult, error) {
	engine, err := kernel.NewFileEffectEngine(store, "cli")
	if err != nil {
		return fileCommandResult{}, err
	}
	verifier, err := kernel.NewFileVerifier(store)
	if err != nil {
		return fileCommandResult{}, err
	}

	effect, err := store.EffectByIdempotency(ctx, request.runID, request.idempotencyKey)
	if fault.IsCategory(err, fault.NotFound) {
		effect, err = createAndStageFileEffect(ctx, store, engine, request)
	} else if err == nil {
		grant, grantErr := store.CapabilityGrant(ctx, effect.CapabilityGrantID)
		if grantErr != nil {
			return fileCommandResult{}, grantErr
		}
		if grant.Scope != request.scope {
			return fileCommandResult{}, fault.New("file.write", fault.Conflict, errors.New("idempotent retry changed capability scope"))
		}
		effect, _, err = engine.Stage(ctx, kernel.FileWriteRequest{
			RunID: effect.RunID, PlanID: effect.PlanID, StepID: effect.StepID,
			CapabilityGrantID: effect.CapabilityGrantID, IdempotencyKey: effect.IdempotencyKey,
			Target: request.target, Content: request.content,
		})
	}
	if err != nil {
		return fileCommandResult{}, err
	}
	if effect.Status == domain.EffectStaged || effect.Status == domain.EffectExecuting {
		effect, err = engine.Execute(ctx, effect.ID)
		if err != nil {
			return fileCommandResult{}, err
		}
	}
	verifiedEffect, verification, err := verifier.Verify(ctx, effect.ID)
	if err != nil {
		return fileCommandResult{}, err
	}
	return fileCommandResult{Effect: verifiedEffect, Verification: verification}, nil
}

func createAndStageFileEffect(ctx context.Context, store *sqlite.Store, engine *kernel.FileEffectEngine, request fileCommandRequest) (domain.Effect, error) {
	run, err := store.Run(ctx, request.runID)
	if err != nil {
		return domain.Effect{}, err
	}
	now := time.Now().UTC()
	milestone, err := domain.NewMilestone("materialize verified filesystem state", nil, nil, now)
	if err != nil {
		return domain.Effect{}, err
	}
	step, err := domain.NewStep(milestone.ID, "write the requested file bytes", now)
	if err != nil {
		return domain.Effect{}, err
	}
	step.Postconditions = []string{"target content matches the desired BLAKE3-256 artifact"}
	step.RequiredCapabilities = []domain.CapabilityRequirement{{Name: domain.FilesystemWriteCapability, Scope: request.scope}}
	step.CandidateTools = []string{"filesystem.write"}
	step.ExpectedEffects = []domain.EffectClass{domain.EffectReversibleWrite}
	step.Verification = domain.VerificationStrategy{Method: "independent file read and BLAKE3-256 hash", RequiredEvidence: []string{"filesystem read", "content digest"}}
	step.CompensationStrategy = "restore the previous staged artifact or remove a newly created file"
	step.ResourceLocks = []string{request.target}
	step.Risk = domain.RiskMedium
	step.FailurePolicy = domain.FailureReplan
	milestone.StepIDs = []string{step.ID}
	plan, err := domain.NewPlan(run.ContractID, []domain.Milestone{milestone}, []domain.Step{step}, now)
	if err != nil {
		return domain.Effect{}, err
	}
	if err := store.CreatePlan(ctx, plan); err != nil {
		return domain.Effect{}, err
	}
	grant, err := domain.NewCapabilityGrant(run.ID, "cli", domain.FilesystemWriteCapability, request.scope,
		domain.EffectReversibleWrite, "explicit owner CLI command", now, now.Add(15*time.Minute))
	if err != nil {
		return domain.Effect{}, err
	}
	if err := store.GrantCapability(ctx, grant); err != nil {
		return domain.Effect{}, err
	}
	effect, _, err := engine.Stage(ctx, kernel.FileWriteRequest{
		RunID: run.ID, PlanID: plan.ID, StepID: step.ID, CapabilityGrantID: grant.ID,
		IdempotencyKey: request.idempotencyKey, Target: request.target, Content: request.content,
	})
	return effect, err
}

func canonicalDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve scope: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve scope symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect scope: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("scope %q is not a directory", path)
	}
	return resolved, nil
}

func runContract(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: azper contract <create|show>")
	}
	switch args[0] {
	case "create":
		flags := flag.NewFlagSet("contract create", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		dbPath := flags.String("db", defaultDBPath(), "SQLite database path")
		objective := flags.String("objective", "", "contract objective")
		var conditions stringList
		flags.Var(&conditions, "success", "required success condition; repeatable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
		}
		contract, err := domain.NewContract(*objective, conditions, time.Now())
		if err != nil {
			return err
		}
		return withStore(ctx, *dbPath, func(store *sqlite.Store) error {
			if err := store.CreateContract(ctx, contract); err != nil {
				return err
			}
			return writeJSON(stdout, contract)
		})
	case "show":
		dbPath, id, err := parseIdentifierFlags("contract show", args[1:])
		if err != nil {
			return err
		}
		return withStore(ctx, dbPath, func(store *sqlite.Store) error {
			contract, err := store.Contract(ctx, id)
			if err != nil {
				return err
			}
			return writeJSON(stdout, contract)
		})
	default:
		return fmt.Errorf("unknown contract command %q", args[0])
	}
}

func runRun(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: azper run <start|show>")
	}
	switch args[0] {
	case "start":
		dbPath, contractID, err := parseIdentifierFlags("run start", args[1:])
		if err != nil {
			return err
		}
		run, err := domain.NewRun(contractID, time.Now())
		if err != nil {
			return err
		}
		return withStore(ctx, dbPath, func(store *sqlite.Store) error {
			if err := store.StartRun(ctx, run); err != nil {
				return err
			}
			return writeJSON(stdout, run)
		})
	case "show":
		dbPath, id, err := parseIdentifierFlags("run show", args[1:])
		if err != nil {
			return err
		}
		return withStore(ctx, dbPath, func(store *sqlite.Store) error {
			run, err := store.Run(ctx, id)
			if err != nil {
				return err
			}
			return writeJSON(stdout, run)
		})
	default:
		return fmt.Errorf("unknown run command %q", args[0])
	}
}

func runTrace(ctx context.Context, args []string, stdout io.Writer) error {
	dbPath, aggregateID, err := parseIdentifierFlags("trace", args)
	if err != nil {
		return err
	}
	return withStore(ctx, dbPath, func(store *sqlite.Store) error {
		events, err := store.Events(ctx, aggregateID)
		if err != nil {
			return err
		}
		return writeJSON(stdout, events)
	})
}

func parseIdentifierFlags(name string, args []string) (string, string, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dbPath := flags.String("db", defaultDBPath(), "SQLite database path")
	if err := flags.Parse(args); err != nil {
		return "", "", err
	}
	if flags.NArg() != 1 {
		return "", "", fmt.Errorf("%s requires exactly one identifier", name)
	}
	return *dbPath, flags.Arg(0), nil
}

func withStore(ctx context.Context, path string, fn func(*sqlite.Store) error) error {
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		return err
	}
	if err := fn(store); err != nil {
		_ = store.Close()
		return err
	}
	if err := store.Close(); err != nil {
		return fmt.Errorf("close SQLite store: %w", err)
	}
	return nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func defaultDBPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "azper.db"
	}
	return filepath.Join(configDir, "azper", "azper.db")
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ", ") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

const usage = `Azper local execution authority

Usage:
  azper contract create [--db PATH] --objective TEXT --success TEXT [--success TEXT]
  azper contract show [--db PATH] CONTRACT_ID
  azper run start [--db PATH] CONTRACT_ID
  azper run show [--db PATH] RUN_ID
  azper trace [--db PATH] AGGREGATE_ID
  azper file write [--db PATH] --run RUN_ID --scope DIR --path FILE --content TEXT --idempotency-key KEY
  azper recover [--db PATH]
`
