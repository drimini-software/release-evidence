package model

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func ValidatePacket(packet Packet) error {
	if packet.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version %q", packet.SchemaVersion)
	}
	if packet.Tool.Name == "" || packet.Tool.Version == "" || packet.Tool.RuleSetVersion == "" {
		return fmt.Errorf("tool identity is incomplete")
	}
	if packet.Repository.Name == "" {
		return fmt.Errorf("repository name is empty")
	}
	if !validState(packet.Repository.RevisionState) || !validState(packet.Repository.WorkingTreeState) {
		return fmt.Errorf("repository state is invalid")
	}
	if packet.Repository.RevisionState == StateObserved && packet.Repository.Revision == "" {
		return fmt.Errorf("observed repository revision is empty")
	}
	if packet.Boundary.Root != "." {
		return fmt.Errorf("packet boundary root must be relative")
	}
	if packet.Boundary.Limits.MaxEntries <= 0 || packet.Boundary.Limits.MaxDepth <= 0 || packet.Boundary.Limits.MaxFileBytes <= 0 || packet.Boundary.Limits.MaxTotalReadBytes <= 0 {
		return fmt.Errorf("packet boundary limits must be positive")
	}
	if _, err := time.Parse(time.RFC3339Nano, packet.Scan.GeneratedAt); err != nil {
		return fmt.Errorf("scan time is invalid: %w", err)
	}

	seen := make(map[string]struct{}, len(packet.Findings))
	lastID := ""
	for index, finding := range packet.Findings {
		if finding.ID == "" || finding.RuleVersion == "" || finding.Category == "" || finding.Title == "" {
			return fmt.Errorf("finding %d identity is incomplete", index)
		}
		if _, duplicate := seen[finding.ID]; duplicate {
			return fmt.Errorf("finding ID %q is duplicated", finding.ID)
		}
		seen[finding.ID] = struct{}{}
		if lastID != "" && finding.ID < lastID {
			return fmt.Errorf("findings are not sorted by ID")
		}
		lastID = finding.ID
		if !validState(finding.State) {
			return fmt.Errorf("finding %q has invalid state", finding.ID)
		}
		if !validConfidence(finding.Confidence) {
			return fmt.Errorf("finding %q has invalid confidence", finding.ID)
		}
		if finding.Explanation == "" || finding.Limitations == "" || len(finding.SearchBoundary) == 0 {
			return fmt.Errorf("finding %q is missing explanatory fields", finding.ID)
		}
		if finding.State == StateObserved && len(finding.Citations) == 0 {
			return fmt.Errorf("observed finding %q has no citation", finding.ID)
		}
		for _, citation := range finding.Citations {
			if err := validateRelativePath(citation.Path); err != nil {
				return fmt.Errorf("finding %q citation: %w", finding.ID, err)
			}
			if citation.Line < 0 {
				return fmt.Errorf("finding %q citation line is invalid", finding.ID)
			}
		}
	}

	for _, diagnostic := range packet.Diagnostics {
		if diagnostic.Code == "" || diagnostic.Message == "" {
			return fmt.Errorf("diagnostic identity is incomplete")
		}
		if diagnostic.Severity != "info" && diagnostic.Severity != "warning" && diagnostic.Severity != "error" {
			return fmt.Errorf("diagnostic %q has invalid severity", diagnostic.Code)
		}
		if diagnostic.Path != "" {
			if err := validateRelativePath(diagnostic.Path); err != nil {
				return fmt.Errorf("diagnostic %q path: %w", diagnostic.Code, err)
			}
		}
	}

	if !sort.StringsAreSorted(packet.Boundary.Exclusions) {
		return fmt.Errorf("boundary exclusions are not sorted")
	}
	if err := validateAssessment(packet.Assessment, seen); err != nil {
		return err
	}
	if err := validateDecision(packet.Decision); err != nil {
		return err
	}
	if packet.Integrity.Algorithm != "sha256" || packet.Integrity.Scope != "normalized-evidence-v1" || len(packet.Integrity.Digest) != 64 {
		return fmt.Errorf("integrity metadata is incomplete")
	}
	return nil
}

func validateAssessment(assessment Assessment, findingIDs map[string]struct{}) error {
	switch assessment.Status {
	case "not_started", "in_progress", "complete":
	default:
		return fmt.Errorf("assessment status is invalid")
	}
	if err := validateText("assessment context", assessment.Context, 4_000); err != nil {
		return err
	}
	if assessment.UpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, assessment.UpdatedAt); err != nil {
			return fmt.Errorf("assessment update time is invalid: %w", err)
		}
	}

	seenItems := make(map[string]struct{}, len(assessment.Items))
	for _, item := range assessment.Items {
		if _, ok := findingIDs[item.FindingID]; !ok {
			return fmt.Errorf("assessment references unknown finding %q", item.FindingID)
		}
		if _, duplicate := seenItems[item.FindingID]; duplicate {
			return fmt.Errorf("assessment repeats finding %q", item.FindingID)
		}
		seenItems[item.FindingID] = struct{}{}
		switch item.Disposition {
		case "open", "assigned", "accepted", "not_applicable", "resolved":
		default:
			return fmt.Errorf("assessment disposition for %q is invalid", item.FindingID)
		}
		if err := validateText("assessment rationale", item.Rationale, 4_000); err != nil {
			return err
		}
		if err := validateText("assessment owner", item.Owner, 200); err != nil {
			return err
		}
		if item.TargetDate != "" {
			if _, err := time.Parse("2006-01-02", item.TargetDate); err != nil {
				return fmt.Errorf("assessment target date for %q is invalid", item.FindingID)
			}
		}
	}
	return nil
}

func validateDecision(decision Decision) error {
	switch decision.Status {
	case "not_made", "approved", "blocked", "needs_work":
	default:
		return fmt.Errorf("decision status is invalid")
	}
	if err := validateText("decision rationale", decision.Rationale, 4_000); err != nil {
		return err
	}
	if err := validateText("decision owner", decision.Owner, 200); err != nil {
		return err
	}
	if decision.UpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, decision.UpdatedAt); err != nil {
			return fmt.Errorf("decision update time is invalid: %w", err)
		}
	}
	return nil
}

func validateText(label, value string, maximum int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", label)
	}
	if utf8.RuneCountInString(value) > maximum {
		return fmt.Errorf("%s exceeds %d characters", label, maximum)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", label)
	}
	return nil
}

func validState(state State) bool {
	switch state {
	case StateObserved, StateNotObserved, StateUnknown, StateNotApplicable:
		return true
	default:
		return false
	}
}

func validConfidence(confidence Confidence) bool {
	switch confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		return true
	default:
		return false
	}
}

func validateRelativePath(value string) error {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("path is empty or contains a NUL byte")
	}
	if strings.ContainsRune(value, '\\') {
		return fmt.Errorf("path is not slash-normalized")
	}
	if path.IsAbs(value) || filepath.IsAbs(value) {
		return fmt.Errorf("path is absolute")
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path escapes or is not normalized")
	}
	return nil
}
