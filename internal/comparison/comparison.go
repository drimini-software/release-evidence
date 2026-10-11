// Package comparison compares recorded evidence, never repository contents.
package comparison

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
)

type Snapshot struct {
	Tool       model.Tool       `json:"tool"`
	Repository model.Repository `json:"repository"`
	Boundary   model.Boundary   `json:"boundary"`
	Scan       model.Scan       `json:"scan"`
	Digest     string           `json:"evidence_digest"`
}

type FindingChange struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Fields []string       `json:"fields"`
	Before *model.Finding `json:"before,omitempty"`
	After  *model.Finding `json:"after,omitempty"`
}

type AssessmentChange struct {
	FindingID string                `json:"finding_id"`
	Before    *model.AssessmentItem `json:"before,omitempty"`
	After     *model.AssessmentItem `json:"after,omitempty"`
}

// Result deliberately separates evidence changes from human assessment changes.
type Result struct {
	SchemaVersion     string             `json:"schema_version"`
	Before            Snapshot           `json:"before"`
	After             Snapshot           `json:"after"`
	Warnings          []string           `json:"warnings"`
	Changes           []FindingChange    `json:"changes"`
	Unchanged         int                `json:"unchanged"`
	Unresolved        []string           `json:"unresolved"`
	AssessmentChanges []AssessmentChange `json:"assessment_changes"`
	ContextChanged    bool               `json:"context_changed"`
	AssessmentBefore  model.Assessment   `json:"assessment_before"`
	AssessmentAfter   model.Assessment   `json:"assessment_after"`
	DecisionChanged   bool               `json:"decision_changed"`
	DecisionBefore    model.Decision     `json:"decision_before"`
	DecisionAfter     model.Decision     `json:"decision_after"`
}

func Compare(before, after model.Packet) (Result, error) {
	for index, p := range []model.Packet{before, after} {
		if err := model.ValidatePacket(p); err != nil {
			return Result{}, fmt.Errorf("packet %d: %w", index+1, err)
		}
		if err := packet.VerifyIntegrity(p); err != nil {
			return Result{}, fmt.Errorf("packet %d: %w", index+1, err)
		}
	}
	result := Result{
		SchemaVersion: "1", Before: snapshot(before), After: snapshot(after),
		Warnings: []string{}, Changes: []FindingChange{}, Unresolved: []string{},
		AssessmentChanges: []AssessmentChange{},
		AssessmentBefore:  before.Assessment, AssessmentAfter: after.Assessment,
		DecisionBefore: before.Decision, DecisionAfter: after.Decision,
		ContextChanged:  before.Assessment.Context != after.Assessment.Context || before.Assessment.Status != after.Assessment.Status,
		DecisionChanged: before.Decision.Status != after.Decision.Status || before.Decision.Owner != after.Decision.Owner || before.Decision.Rationale != after.Decision.Rationale,
	}
	if before.Repository.Name != after.Repository.Name {
		result.Warnings = append(result.Warnings, "Repository names differ. Confirm that these packets describe the intended repository.")
	}
	if before.Tool != after.Tool {
		result.Warnings = append(result.Warnings, "Tool or rule-set versions differ. Changes may reflect detector behavior rather than repository changes.")
	}
	if !reflect.DeepEqual(before.Boundary.Limits, after.Boundary.Limits) || !equalStrings(before.Boundary.Exclusions, after.Boundary.Exclusions) {
		result.Warnings = append(result.Warnings, "Scan limits or exclusions differ. The evidence was gathered within different boundaries.")
	}
	if !before.Scan.Complete || !after.Scan.Complete {
		result.Warnings = append(result.Warnings, "At least one scan is incomplete. Missing or removed evidence cannot establish absence.")
	}
	if !reflect.DeepEqual(before.Diagnostics, after.Diagnostics) {
		result.Warnings = append(result.Warnings, "Scan diagnostics differ. Review both packets before interpreting changes.")
	}
	if before.Repository.RevisionState != model.StateObserved || after.Repository.RevisionState != model.StateObserved {
		result.Warnings = append(result.Warnings, "At least one repository revision is unknown. The packets cannot be tied to two verified revisions.")
	}
	if before.Repository.WorkingTreeState != model.StateObserved || after.Repository.WorkingTreeState != model.StateObserved {
		result.Warnings = append(result.Warnings, "At least one working tree is dirty or unknown. Revision identifiers alone do not describe the scanned contents.")
	}
	previous, current := findingMap(before.Findings), findingMap(after.Findings)
	for _, id := range unionIDs(previous, current) {
		old, hadOld := previous[id]
		next, hasNext := current[id]
		switch {
		case !hadOld:
			result.Changes = append(result.Changes, FindingChange{ID: id, Kind: "added", Fields: []string{}, After: &next})
		case !hasNext:
			result.Changes = append(result.Changes, FindingChange{ID: id, Kind: "removed", Fields: []string{}, Before: &old})
		default:
			fields := changedFields(old, next)
			if len(fields) == 0 {
				result.Unchanged++
			} else {
				result.Changes = append(result.Changes, FindingChange{ID: id, Kind: "changed", Fields: fields, Before: &old, After: &next})
			}
		}
		if hasNext && (next.State == model.StateUnknown || next.State == model.StateNotObserved) {
			result.Unresolved = append(result.Unresolved, id)
		}
	}
	oldItems, newItems := assessmentMap(before.Assessment.Items), assessmentMap(after.Assessment.Items)
	ids := map[string]bool{}
	for id := range oldItems {
		ids[id] = true
	}
	for id := range newItems {
		ids[id] = true
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		old, hadOld := oldItems[id]
		next, hasNext := newItems[id]
		if hadOld && hasNext && old == next {
			continue
		}
		change := AssessmentChange{FindingID: id}
		if hadOld {
			change.Before = &old
		}
		if hasNext {
			change.After = &next
		}
		result.AssessmentChanges = append(result.AssessmentChanges, change)
	}
	return result, nil
}

func snapshot(p model.Packet) Snapshot {
	return Snapshot{Tool: p.Tool, Repository: p.Repository, Boundary: p.Boundary, Scan: p.Scan, Digest: p.Integrity.Digest}
}

func findingMap(findings []model.Finding) map[string]model.Finding {
	result := make(map[string]model.Finding, len(findings))
	for _, finding := range findings {
		result[finding.ID] = finding
	}
	return result
}

func assessmentMap(items []model.AssessmentItem) map[string]model.AssessmentItem {
	result := make(map[string]model.AssessmentItem, len(items))
	for _, item := range items {
		result[item.FindingID] = item
	}
	return result
}

func unionIDs(a, b map[string]model.Finding) []string {
	ids := map[string]bool{}
	for id := range a {
		ids[id] = true
	}
	for id := range b {
		ids[id] = true
	}
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func changedFields(a, b model.Finding) []string {
	fields := []string{}
	for _, pair := range []struct {
		name    string
		changed bool
	}{
		{"rule_version", a.RuleVersion != b.RuleVersion}, {"category", a.Category != b.Category},
		{"title", a.Title != b.Title}, {"state", a.State != b.State}, {"confidence", a.Confidence != b.Confidence},
		{"explanation", a.Explanation != b.Explanation}, {"limitations", a.Limitations != b.Limitations},
		{"search_boundary", !equalStrings(a.SearchBoundary, b.SearchBoundary)}, {"citations", !equalCitations(a.Citations, b.Citations)},
	} {
		if pair.changed {
			fields = append(fields, pair.name)
		}
	}
	return fields
}

func equalStrings(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

func equalCitations(a, b []model.Citation) bool {
	a, b = append([]model.Citation(nil), a...), append([]model.Citation(nil), b...)
	less := func(c []model.Citation) func(int, int) bool {
		return func(i, j int) bool {
			if c[i].Path == c[j].Path {
				return c[i].Line < c[j].Line
			}
			return c[i].Path < c[j].Path
		}
	}
	sort.Slice(a, less(a))
	sort.Slice(b, less(b))
	return reflect.DeepEqual(a, b)
}
