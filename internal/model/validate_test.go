package model

import "testing"

func TestValidatePacketRejectsEscapingCitation(t *testing.T) {
	packet := minimallyValidPacket()
	packet.Findings[0].Citations[0].Path = "../secret"
	if err := ValidatePacket(packet); err == nil {
		t.Fatal("ValidatePacket() accepted an escaping citation")
	}
}

func minimallyValidPacket() Packet {
	return Packet{
		SchemaVersion: SchemaVersion,
		Tool:          Tool{Name: "release-evidence", Version: "test", RuleSetVersion: "1"},
		Repository: Repository{
			Name:             "fixture",
			RevisionState:    StateUnknown,
			WorkingTreeState: StateUnknown,
			WorkingTreeNote:  "Not inspected.",
		},
		Boundary: Boundary{
			Root:       ".",
			Exclusions: []string{},
			Limits: Limits{
				MaxEntries:        1,
				MaxDepth:          1,
				MaxFileBytes:      1,
				MaxTotalReadBytes: 1,
			},
		},
		Scan: Scan{GeneratedAt: "2026-09-27T00:00:00Z", Complete: true},
		Findings: []Finding{{
			ID:             "fixture.rule",
			RuleVersion:    "1",
			Category:       "fixture",
			Title:          "Fixture",
			State:          StateObserved,
			Confidence:     ConfidenceHigh,
			Explanation:    "Fixture evidence was observed.",
			Limitations:    "Fixture only.",
			SearchBoundary: []string{"fixture"},
			Citations:      []Citation{{Path: "README.md"}},
		}},
		Diagnostics: []Diagnostic{},
		Assessment:  Assessment{Status: "not_started"},
		Decision:    Decision{Status: "not_made"},
		Integrity: Integrity{
			Algorithm: "sha256",
			Scope:     "normalized-evidence-v1",
			Digest:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Note:      "Fixture digest.",
		},
	}
}
