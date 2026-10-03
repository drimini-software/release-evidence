package detector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/repository"
)

func TestCompleteFixtureMatchesGoldenSummary(t *testing.T) {
	snapshot, err := repository.Inspect(context.Background(), fixturePath(t, "complete"), repository.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	findings := Evaluate(snapshot)

	var lines []string
	for _, finding := range findings {
		citations := make([]string, 0, len(finding.Citations))
		for _, citation := range finding.Citations {
			citations = append(citations, citation.Path)
		}
		lines = append(lines, fmt.Sprintf("%s=%s|%s", finding.ID, finding.State, strings.Join(citations, ",")))
	}
	actual := strings.Join(lines, "\n") + "\n"
	expected, err := os.ReadFile(filepath.Join("testdata", "complete.findings.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if actual != string(expected) {
		t.Fatalf("finding summary mismatch\nactual:\n%s\nexpected:\n%s", actual, string(expected))
	}
}

func TestIncompleteFixtureDoesNotClaimObservedEvidence(t *testing.T) {
	snapshot, err := repository.Inspect(context.Background(), fixturePath(t, "incomplete"), repository.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range Evaluate(snapshot) {
		if finding.State != model.StateNotObserved {
			t.Fatalf("finding %s state = %s, want not_observed", finding.ID, finding.State)
		}
		if len(finding.Citations) != 0 {
			t.Fatalf("finding %s has citations without observed evidence", finding.ID)
		}
	}
}

func TestMisleadingNarrativeDoesNotBecomeStructuredEvidence(t *testing.T) {
	snapshot, err := repository.Inspect(context.Background(), fixturePath(t, "misleading"), repository.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range Evaluate(snapshot) {
		if finding.ID == "build.manifest" {
			if finding.State != model.StateObserved {
				t.Fatalf("build manifest state = %s, want observed", finding.State)
			}
			continue
		}
		if finding.State != model.StateNotObserved {
			t.Fatalf("narrative caused finding %s state = %s, want not_observed", finding.ID, finding.State)
		}
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
