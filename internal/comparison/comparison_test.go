package comparison

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
	"github.com/drimini-software/release-evidence/internal/scanner"
)

func fixture(t *testing.T) model.Packet {
	t.Helper()
	p, err := scanner.Scan(context.Background(), filepath.Join("..", "..", "fixtures", "session"), scanner.Options{ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func clone(t *testing.T, p model.Packet) model.Packet {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var copy model.Packet
	if err := json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func seal(t *testing.T, p *model.Packet) {
	t.Helper()
	if err := packet.SetIntegrity(p); err != nil {
		t.Fatal(err)
	}
}

func TestCompareSeparatesEvidenceFromHumanChangesAndTimestamps(t *testing.T) {
	before := fixture(t)
	after := clone(t, before)
	after.Scan.GeneratedAt = "2026-10-11T10:00:00Z"
	after.Assessment.UpdatedAt = "2026-10-11T10:00:00Z"
	after.Decision.UpdatedAt = "2026-10-11T10:00:00Z"
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 || result.Unchanged != len(before.Findings) || result.DecisionChanged || result.ContextChanged {
		t.Fatal("timestamps became semantic changes")
	}
	after.Assessment.Context = "A handover review"
	after.Assessment.Items = []model.AssessmentItem{{FindingID: after.Findings[0].ID, Disposition: "not_applicable", Rationale: "Outside this decision", Owner: "Reviewer"}}
	after.Decision.Status = "needs_work"
	result, err = Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 || len(result.AssessmentChanges) != 1 || !result.DecisionChanged || !result.ContextChanged {
		t.Fatal("human changes lost or counted as evidence changes")
	}
	if after.Findings[0].State != before.Findings[0].State {
		t.Fatal("human applicability changed machine state")
	}
}

func TestCompareClassifiesAddedRemovedChangedAndUnresolvedEvidence(t *testing.T) {
	before := fixture(t)
	after := clone(t, before)
	removedID := after.Findings[0].ID
	after.Findings = after.Findings[1:]
	after.Findings[0].State = model.StateUnknown
	changedID := after.Findings[0].ID
	newFinding := after.Findings[len(after.Findings)-1]
	newFinding.ID = "zz.added"
	after.Findings = append(after.Findings, newFinding)
	seal(t, &after)
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{removedID: "removed", changedID: "changed", "zz.added": "added"}
	if len(result.Changes) != len(want) {
		t.Fatalf("changes = %+v", result.Changes)
	}
	for _, change := range result.Changes {
		if change.Kind != want[change.ID] {
			t.Fatalf("unexpected classification: %+v", change)
		}
	}
	if !contains(result.Unresolved, changedID) {
		t.Fatal("current unknown evidence is missing")
	}
}

func TestCompareIgnoresCitationAndSearchOrderWithoutMutatingPackets(t *testing.T) {
	before := fixture(t)
	before.Findings[0].Citations = []model.Citation{{Path: "one.txt", Line: 1}, {Path: "two.txt", Line: 2}}
	before.Findings[0].SearchBoundary = []string{"one", "two"}
	seal(t, &before)
	after := clone(t, before)
	after.Findings[0].Citations[0], after.Findings[0].Citations[1] = after.Findings[0].Citations[1], after.Findings[0].Citations[0]
	after.Findings[0].SearchBoundary[0], after.Findings[0].SearchBoundary[1] = after.Findings[0].SearchBoundary[1], after.Findings[0].SearchBoundary[0]
	seal(t, &after)
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 {
		t.Fatal("collection order counted as an evidence change")
	}
	if after.Findings[0].Citations[0].Path != "two.txt" || after.Findings[0].SearchBoundary[0] != "two" {
		t.Fatal("comparison mutated the input")
	}
}

func TestCompareWarnsAboutDifferentBoundariesVersionsAndIncompleteScans(t *testing.T) {
	before := fixture(t)
	after := clone(t, before)
	after.Scan.Complete = false
	after.Tool.RuleSetVersion = "different"
	after.Boundary.Limits.MaxEntries++
	seal(t, &after)
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(result.Warnings, " ")
	for _, text := range []string{"versions differ", "boundaries", "incomplete"} {
		if !strings.Contains(joined, text) {
			t.Fatalf("missing %q warning: %s", text, joined)
		}
	}
}

func TestCompareRejectsTamperedPackets(t *testing.T) {
	before := fixture(t)
	after := clone(t, before)
	after.Findings[0].Explanation = "Changed without updating the digest"
	if _, err := Compare(before, after); err == nil {
		t.Fatal("tampered evidence was accepted")
	}
}

func TestCompareDetectsChangedCitationWithUnchangedState(t *testing.T) {
	before := fixture(t)
	after := clone(t, before)
	after.Findings[0].Citations = []model.Citation{{Path: "new-evidence.txt", Line: 7}}
	seal(t, &after)
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || !contains(result.Changes[0].Fields, "citations") {
		t.Fatal("changed evidence location was ignored")
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
