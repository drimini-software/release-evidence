package report

import (
	"bytes"
	"context"
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
	p.Scan.GeneratedAt = "2026-10-11T10:00:00Z"
	return p
}

func TestHTMLRendersHostileTextAsInertContent(t *testing.T) {
	p := fixture(t)
	attack := `<script>window.injected=true</script><img src="https://attacker.invalid/pixel" onerror="alert(1)">`
	p.Repository.Name = attack
	p.Findings[0].Title = attack
	p.Findings[0].Explanation = attack
	p.Findings[0].Citations = []model.Citation{{Path: "<svg onload=alert(1)>.txt"}}
	p.Assessment.Context = attack
	p.Assessment.Items = []model.AssessmentItem{{FindingID: p.Findings[0].ID, Disposition: "assigned", Rationale: attack, Owner: attack}}
	p.Decision.Rationale = attack
	if err := packet.SetIntegrity(&p); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Render(&output, p, nil, "html"); err != nil {
		t.Fatal(err)
	}
	for _, active := range []string{"<script", "<img", "<svg", `<a href="https://attacker`, `<iframe`} {
		if strings.Contains(output.String(), active) {
			t.Fatalf("active repository markup survived: %s", active)
		}
	}
	if !strings.Contains(output.String(), "&lt;script&gt;") {
		t.Fatal("hostile text was removed rather than rendered as text")
	}
	if !strings.Contains(output.String(), "default-src &#39;none&#39;") && !strings.Contains(output.String(), "default-src 'none'") {
		t.Fatal("offline report lacks its CSP")
	}
}

func TestMarkdownCannotCreateInjectedBlocksLinksOrImages(t *testing.T) {
	p := fixture(t)
	p.Assessment.Context = "` `` ```\n# injected heading\n![remote image](https://attacker.invalid/pixel)\n<script>alert(1)</script>"
	var output bytes.Buffer
	if err := Render(&output, p, nil, "markdown"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\n# injected heading") || strings.Contains(output.String(), "\n![remote image]") || strings.Contains(output.String(), "\n<script>") {
		t.Fatal("untrusted text escaped into a Markdown block")
	}
	if !strings.Contains(output.String(), "```` ` `` ``` ⏎ # injected heading") {
		t.Fatal("hostile delimiter text was not enclosed in a longer code span")
	}
}

func TestReportsRetainIncompleteBoundaryAndSeparateHumanDecision(t *testing.T) {
	p := fixture(t)
	p.Scan.Complete = false
	p.Assessment.Status = "in_progress"
	p.Assessment.Items = []model.AssessmentItem{{FindingID: p.Findings[0].ID, Disposition: "not_applicable", Rationale: "Outside this review", Owner: "Reviewer"}}
	p.Decision.Status, p.Decision.Owner = "approved", "Reviewer"
	if err := packet.SetIntegrity(&p); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"html", "markdown"} {
		var output bytes.Buffer
		if err := Render(&output, p, nil, format); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"Incomplete scan", "Scanner observation", "Human disposition", "not applicable", "approved", "Active exclusions", "Maximum total read bytes", p.Integrity.Digest, "not a signature"} {
			if !strings.Contains(output.String(), text) {
				t.Fatalf("%s report missing %q", format, text)
			}
		}
	}
}

func TestReportIsDeterministicAndContainsNoLocalAbsoluteRoot(t *testing.T) {
	p := fixture(t)
	for _, format := range []string{"html", "markdown"} {
		var first, second bytes.Buffer
		if err := Render(&first, p, nil, format); err != nil {
			t.Fatal(err)
		}
		if err := Render(&second, p, nil, format); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("same packet produced different exports")
		}
		root, err := filepath.Abs(filepath.Join("..", "..", "fixtures", "session"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(first.String(), root) {
			t.Fatal("absolute local root leaked into export")
		}
	}
}

func TestReportRejectsInvalidOrTamperedPacketsBeforeWriting(t *testing.T) {
	for _, mutate := range []func(*model.Packet){
		func(p *model.Packet) { p.SchemaVersion = "999" },
		func(p *model.Packet) { p.Findings[0].Citations = []model.Citation{{Path: "/private/secret"}} },
		func(p *model.Packet) { p.Findings[0].Explanation = "tampered evidence" },
	} {
		p := fixture(t)
		mutate(&p)
		var output bytes.Buffer
		if err := Render(&output, p, nil, "html"); err == nil {
			t.Fatal("invalid packet accepted")
		}
		if output.Len() != 0 {
			t.Fatal("invalid packet partially rendered")
		}
	}
}

func TestComparisonReportPreservesPreviousRemovedAndCurrentEvidence(t *testing.T) {
	before := fixture(t)
	after := fixture(t)
	removedID := after.Findings[0].ID
	after.Findings = after.Findings[1:]
	after.Assessment.Context = "Changed review context"
	after.Decision.Status = "needs_work"
	if err := packet.SetIntegrity(&after); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"html", "markdown"} {
		var output bytes.Buffer
		if err := Render(&output, after, &before, format); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"Changes between packets", "removed", removedID, "Changed review context", "Human decision changed", "Current unknown or not-observed evidence"} {
			if !strings.Contains(output.String(), text) {
				t.Fatalf("%s comparison missing %q", format, text)
			}
		}
	}
}
