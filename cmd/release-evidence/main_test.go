package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
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

func scanPacket(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packet.json")
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"scan", "--output", path, filepath.Join("..", "..", "fixtures", "session")}, &stdout, &stderr); code != 0 {
		t.Fatalf("scan exit %d: %s", code, stderr.String())
	}
	return path
}

func TestExportWorksFromSavedPacketWithoutRepository(t *testing.T) {
	input := scanPacket(t)
	for _, format := range []string{"html", "markdown"} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"export", "--format", format, input}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("export exit %d: %s", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Decision record") || !strings.Contains(stdout.String(), "Scan boundary and provenance") {
			t.Fatal("export is missing report sections")
		}
		if strings.Contains(stdout.String(), filepath.Dir(input)) {
			t.Fatal("input packet absolute path leaked into report")
		}
	}
}

func TestExportCannotOverwriteInputOrDestroyPreviousReportOnInvalidPacket(t *testing.T) {
	input := scanPacket(t)
	previousInput, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"export", "--output", input, input}, &stdout, &stderr); code != 1 {
		t.Fatalf("input overwrite exit = %d", code)
	}
	after, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(previousInput, after) {
		t.Fatal("input was changed")
	}
	destination := filepath.Join(t.TempDir(), "report.html")
	if err := os.WriteFile(destination, []byte("previous report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte(`{"schema_version":"999"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"export", "--output", destination, input}, &stdout, &stderr); code != 1 {
		t.Fatalf("invalid packet exit = %d", code)
	}
	after, err = os.ReadFile(destination)
	if err != nil || string(after) != "previous report" {
		t.Fatal("invalid packet destroyed previous report")
	}
}

func TestCompareJSONAndBothReportFormats(t *testing.T) {
	input := scanPacket(t)
	current, err := packet.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	current.Decision.Status = "needs_work"
	current.Decision.Rationale = "An independently unvalidated test decision"
	after := filepath.Join(t.TempDir(), "current.json")
	if err := packet.WriteAtomic(after, current, true); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"html", "markdown", "json"} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{"compare", "--format", format, input, after}, &stdout, &stderr); code != 0 {
			t.Fatalf("compare %s exit %d: %s", format, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), current.Decision.Rationale) {
			t.Fatal("comparison lost current human decision")
		}
		if format == "json" {
			var result struct {
				DecisionChanged bool              `json:"decision_changed"`
				Changes         []json.RawMessage `json:"changes"`
				Unchanged       int               `json:"unchanged"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.DecisionChanged || len(result.Changes) != 0 || result.Unchanged != len(current.Findings) {
				t.Fatal("human decision was counted as an evidence change")
			}
		}
	}
}

func TestReportCommandsRejectInvalidOptions(t *testing.T) {
	for _, args := range [][]string{
		{"export"}, {"compare", "one.json"}, {"export", "--format", "json", "packet.json"},
		{"compare", "--format", "pdf", "one.json", "two.json"}, {"export", "--output=", "packet.json"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), args, &stdout, &stderr); code != 2 {
			t.Fatalf("%v exit %d: %s", args, code, stderr.String())
		}
	}
}

func TestReportCommandsPreserveOutputOnAmbiguousPacket(t *testing.T) {
	validPath := scanPacket(t)
	valid, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := bytes.Replace(valid, []byte(`"status": "not_made"`), []byte(`"status": "approved", "status": "not_made"`), 1)
	if bytes.Equal(valid, ambiguous) {
		t.Fatal("test did not add a duplicate decision field")
	}
	invalidPath := filepath.Join(t.TempDir(), "ambiguous.json")
	if err := os.WriteFile(invalidPath, ambiguous, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"export", invalidPath},
		{"compare", invalidPath, validPath},
		{"compare", validPath, invalidPath},
	} {
		destination := filepath.Join(t.TempDir(), "report.html")
		if err := os.WriteFile(destination, []byte("previous report"), 0o600); err != nil {
			t.Fatal(err)
		}
		command := append([]string{args[0], "--output", destination}, args[1:]...)
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), command, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
			t.Fatalf("ambiguous packet command did not fail safely: %v, exit %d", command, code)
		}
		after, err := os.ReadFile(destination)
		if err != nil || string(after) != "previous report" {
			t.Fatal("ambiguous packet replaced the previous report")
		}
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
