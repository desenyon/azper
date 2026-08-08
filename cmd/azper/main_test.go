package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"azper/internal/domain"
)

func TestCLICreatesContractStartsRunAndTracesBoth(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "azper.db")
	var output bytes.Buffer

	err := run(ctx, []string{
		"contract", "create", "--db", dbPath,
		"--objective", "durably execute work",
		"--success", "contract survives restart",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	var contract domain.Contract
	if err := json.Unmarshal(output.Bytes(), &contract); err != nil {
		t.Fatal(err)
	}

	output.Reset()
	if err := run(ctx, []string{"run", "start", "--db", dbPath, contract.ID}, &output); err != nil {
		t.Fatal(err)
	}
	var started domain.Run
	if err := json.Unmarshal(output.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.ContractID != contract.ID || started.Status != domain.RunRunning {
		t.Fatalf("unexpected run: %#v", started)
	}

	output.Reset()
	if err := run(ctx, []string{"trace", "--db", dbPath, started.ID}, &output); err != nil {
		t.Fatal(err)
	}
	var events []domain.Event
	if err := json.Unmarshal(output.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != domain.EventRunStarted {
		t.Fatalf("unexpected run trace: %#v", events)
	}
}

func TestCLIRejectsContractWithoutSuccessCondition(t *testing.T) {
	err := run(context.Background(), []string{
		"contract", "create", "--db", filepath.Join(t.TempDir(), "azper.db"),
		"--objective", "cannot claim vague success",
	}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCLIFileWriteCommitsOnlyAfterIndependentVerification(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "azper.db")
	scope := t.TempDir()
	var output bytes.Buffer

	if err := run(ctx, []string{
		"contract", "create", "--db", dbPath,
		"--objective", "write verified output", "--success", "output bytes match",
	}, &output); err != nil {
		t.Fatal(err)
	}
	var contract domain.Contract
	if err := json.Unmarshal(output.Bytes(), &contract); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run(ctx, []string{"run", "start", "--db", dbPath, contract.ID}, &output); err != nil {
		t.Fatal(err)
	}
	var started domain.Run
	if err := json.Unmarshal(output.Bytes(), &started); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"file", "write", "--db", dbPath, "--run", started.ID, "--scope", scope,
		"--path", "output.txt", "--content", "verified bytes", "--idempotency-key", "output-v1",
	}
	output.Reset()
	if err := run(ctx, args, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Effect       domain.Effect       `json:"effect"`
		Verification domain.Verification `json:"verification"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Effect.Status != domain.EffectCommitted || result.Verification.Status != domain.VerificationPassed {
		t.Fatalf("effect = %s verification = %s", result.Effect.Status, result.Verification.Status)
	}
	content, err := os.ReadFile(filepath.Join(scope, "output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "verified bytes" {
		t.Fatalf("file content = %q", content)
	}

	firstID := result.Effect.ID
	output.Reset()
	if err := run(ctx, args, &output); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Effect.ID != firstID {
		t.Fatalf("retry created effect %q, want %q", result.Effect.ID, firstID)
	}

	changed := append([]string(nil), args...)
	changed[11] = "different bytes"
	if err := run(ctx, changed, &bytes.Buffer{}); err == nil {
		t.Fatal("expected changed idempotent request to fail")
	}
}

func TestCLIRecoverReportsEmptyQueue(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"recover", "--db", filepath.Join(t.TempDir(), "azper.db")}, &output); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Inspected int `json:"inspected"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Inspected != 0 {
		t.Fatalf("inspected = %d, want 0", report.Inspected)
	}
}
