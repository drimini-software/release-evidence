package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/drimini-software/release-evidence/internal/model"
)

func TestRunScanWritesPacketToStandardOutput(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "complete"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"scan", "--pretty=false", fixture}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var packet model.Packet
	if err := json.Unmarshal(stdout.Bytes(), &packet); err != nil {
		t.Fatalf("scan output is not JSON: %v\n%s", err, stdout.String())
	}
	if packet.SchemaVersion != model.SchemaVersion || len(packet.Findings) == 0 {
		t.Fatal("scan output is missing schema or findings")
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run(context.Background(), []string{"launch"}, &stdout, &stderr); exitCode != 2 {
		t.Fatalf("run() exit code = %d, want 2", exitCode)
	}
}

func TestRunReviewRejectsNonPositiveIdleTimeout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{"review", "--idle-timeout=0s", "."}
	if exitCode := run(context.Background(), args, &stdout, &stderr); exitCode != 2 {
		t.Fatalf("run() exit code = %d, want 2", exitCode)
	}
	if got := stderr.String(); got != "review limits must be positive\n" {
		t.Fatalf("stderr = %q", got)
	}
}
