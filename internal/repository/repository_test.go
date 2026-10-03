package repository

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/drimini-software/release-evidence/internal/model"
)

func TestInspectHonorsIgnoreRulesAndSecretDefaults(t *testing.T) {
	snapshot, err := Inspect(context.Background(), fixturePath(t, "complete"), DefaultOptions())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !snapshot.Complete {
		t.Fatal("Inspect() marked complete fixture incomplete")
	}

	joined := strings.Join(snapshot.Paths, "\n")
	for _, forbidden := range []string{"ignored/hidden-package.json", "ignored.log"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("ignored path %q was considered", forbidden)
		}
	}
	for _, expected := range []string{"CODEOWNERS", "package.json", "tests/site.spec.ts"} {
		if !contains(snapshot.Paths, expected) {
			t.Fatalf("expected path %q was not considered", expected)
		}
	}

	hostile, err := Inspect(context.Background(), fixturePath(t, "hostile"), DefaultOptions())
	if err != nil {
		t.Fatalf("Inspect(hostile) error = %v", err)
	}
	if contains(hostile.Paths, ".env") {
		t.Fatal("local environment file was considered")
	}
}

func TestInspectStopsAtEntryBudget(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	options := DefaultOptions()
	options.MaxEntries = 1
	snapshot, err := Inspect(context.Background(), root, options)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if snapshot.Complete {
		t.Fatal("budget-limited scan was marked complete")
	}
	if !hasDiagnostic(snapshot.Diagnostics, "entry_budget_exceeded") {
		t.Fatal("entry budget diagnostic was not recorded")
	}
}

func TestInspectDoesNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secretPath := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(secretPath, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, "linked.txt")
	if err := os.Symlink(secretPath, linkPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating symlinks requires additional Windows privileges: %v", err)
		}
		t.Fatal(err)
	}

	snapshot, err := Inspect(context.Background(), root, DefaultOptions())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if contains(snapshot.Paths, "linked.txt") {
		t.Fatal("symbolic link was considered as a regular file")
	}
	if !hasDiagnostic(snapshot.Diagnostics, "link_skipped") {
		t.Fatal("link skip diagnostic was not recorded")
	}
}

func TestInspectReadsOnlyInRootGitRevision(t *testing.T) {
	root := t.TempDir()
	refDirectory := filepath.Join(root, ".git", "refs", "heads")
	if err := os.MkdirAll(refDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	if err := os.WriteFile(filepath.Join(refDirectory, "main"), []byte(revision+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Inspect(context.Background(), root, DefaultOptions())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if snapshot.RevisionState != model.StateObserved || snapshot.Revision != revision {
		t.Fatalf("revision = %q (%s), want %q (observed)", snapshot.Revision, snapshot.RevisionState, revision)
	}
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func hasDiagnostic(values []model.Diagnostic, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}
