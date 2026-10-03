package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drimini-software/release-evidence/internal/repository"
)

func TestScanIsDeterministicForFixedInputs(t *testing.T) {
	fixed := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	options := Options{
		Repository:  repository.DefaultOptions(),
		ToolVersion: "test",
		Now:         func() time.Time { return fixed },
	}
	root := fixturePath(t, "complete")

	first, err := Scan(context.Background(), root, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Scan(context.Background(), root, options)
	if err != nil {
		t.Fatal(err)
	}

	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("identical input produced different normalized packets")
	}
	if first.Integrity.Digest == "" || first.Integrity.Digest != second.Integrity.Digest {
		t.Fatal("integrity digest is empty or unstable")
	}
}

func TestScanDoesNotLeakExcludedSecretValue(t *testing.T) {
	result, err := Scan(context.Background(), fixturePath(t, "hostile"), Options{
		Repository:  repository.DefaultOptions(),
		ToolVersion: "test",
		Now:         func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must-never-appear-in-a-packet") {
		t.Fatal("excluded secret value appeared in packet output")
	}
}

func TestScanRejectsMissingRoot(t *testing.T) {
	_, err := Scan(context.Background(), filepath.Join(t.TempDir(), "missing"), Options{})
	if err == nil {
		t.Fatal("Scan() accepted a missing root")
	}
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}
