package packet

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drimini-software/release-evidence/internal/model"
)

func TestEncodeEscapesRepositoryControlledHTML(t *testing.T) {
	packet := model.Packet{Repository: model.Repository{Name: "<script>alert(1)</script>"}}
	var output bytes.Buffer
	if err := Encode(&output, packet, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "<script>") {
		t.Fatal("JSON output contains unescaped active-looking markup")
	}
	if !strings.Contains(output.String(), `\u003cscript\u003e`) {
		t.Fatal("JSON output did not preserve the value in escaped form")
	}
}

func TestWriteAtomicReplacesExistingPacket(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "packet.json")
	if err := os.WriteFile(destination, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	packet := model.Packet{SchemaVersion: "replacement"}
	if err := WriteAtomic(destination, packet, true); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "previous") || !strings.Contains(string(content), "replacement") {
		t.Fatalf("unexpected replacement content: %s", content)
	}
}

func TestDecodeRejectsTamperedIntegrity(t *testing.T) {
	packet := model.Packet{
		SchemaVersion: model.SchemaVersion,
		Tool:          model.Tool{Name: "release-evidence", Version: "test", RuleSetVersion: "1"},
		Repository: model.Repository{
			Name:             "fixture",
			RevisionState:    model.StateUnknown,
			WorkingTreeState: model.StateUnknown,
			WorkingTreeNote:  "Not inspected.",
		},
		Boundary: model.Boundary{
			Root:       ".",
			Exclusions: []string{},
			Limits: model.Limits{
				MaxEntries:        1,
				MaxDepth:          1,
				MaxFileBytes:      1,
				MaxTotalReadBytes: 1,
			},
		},
		Scan:        model.Scan{GeneratedAt: "2026-09-27T00:00:00Z", Complete: true},
		Findings:    []model.Finding{},
		Diagnostics: []model.Diagnostic{},
		Assessment:  model.Assessment{Status: "not_started"},
		Decision:    model.Decision{Status: "not_made"},
	}
	if err := SetIntegrity(&packet); err != nil {
		t.Fatal(err)
	}
	packet.Repository.Name = "tampered"
	var encoded bytes.Buffer
	if err := Encode(&encoded, packet, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(&encoded); err == nil {
		t.Fatal("Decode() accepted a packet whose normalized evidence was changed")
	}
}
