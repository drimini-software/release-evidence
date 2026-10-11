// Package report renders validated packets without opening the repository.
package report

import (
	"embed"
	"fmt"
	htmltemplate "html/template"
	"io"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/drimini-software/release-evidence/internal/comparison"
	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/packet"
)

//go:embed report.html report.md
var templates embed.FS

var htmlReport = htmltemplate.Must(htmltemplate.New("report.html").Funcs(htmltemplate.FuncMap{
	"label": label,
}).ParseFS(templates, "report.html"))

var markdownReport = texttemplate.Must(texttemplate.New("report.md").Funcs(texttemplate.FuncMap{
	"label": label, "value": markdownValue,
}).ParseFS(templates, "report.md"))

type field struct{ Label, Value string }
type count struct {
	Label string
	Total int
}
type findingView struct {
	model.Finding
	Index    int
	Human    model.AssessmentItem
	HasHuman bool
}
type document struct {
	Packet         model.Packet
	Metadata       []field
	Counts         []count
	Findings       []findingView
	OpenItems      []findingView
	Comparison     *comparison.Result
	BeforeMetadata []field
}

// Render rejects unsupported packets and evidence with a mismatched digest.
// HTML is self-contained and script-free; Markdown keeps every variable value
// inside a code span so it cannot create links, images, headings, or raw HTML.
func Render(writer io.Writer, current model.Packet, previous *model.Packet, format string) error {
	if format != "html" && format != "markdown" {
		return fmt.Errorf("report format must be html or markdown")
	}
	if err := model.ValidatePacket(current); err != nil {
		return err
	}
	if err := packet.VerifyIntegrity(current); err != nil {
		return err
	}
	doc := document{Packet: current, Metadata: metadata(current)}
	if previous != nil {
		result, err := comparison.Compare(*previous, current)
		if err != nil {
			return err
		}
		doc.Comparison = &result
		doc.BeforeMetadata = metadata(*previous)
	}
	human := map[string]model.AssessmentItem{}
	for _, item := range current.Assessment.Items {
		human[item.FindingID] = item
	}
	states := map[model.State]int{}
	for index, finding := range current.Findings {
		item, exists := human[finding.ID]
		view := findingView{Finding: finding, Index: index + 1, Human: item, HasHuman: exists}
		doc.Findings = append(doc.Findings, view)
		states[finding.State]++
		if !exists || item.Disposition == "open" || item.Disposition == "assigned" {
			doc.OpenItems = append(doc.OpenItems, view)
		}
	}
	for _, state := range []model.State{model.StateObserved, model.StateNotObserved, model.StateUnknown, model.StateNotApplicable} {
		doc.Counts = append(doc.Counts, count{Label: label(string(state)), Total: states[state]})
	}
	if format == "html" {
		return htmlReport.Execute(writer, doc)
	}
	return markdownReport.Execute(writer, doc)
}

func metadata(p model.Packet) []field {
	revision := p.Repository.Revision
	if revision == "" {
		revision = "Not recorded"
	}
	scanTime, _ := time.Parse(time.RFC3339Nano, p.Scan.GeneratedAt)
	return []field{
		{"Repository", p.Repository.Name}, {"Revision", revision}, {"Revision observation", label(string(p.Repository.RevisionState))},
		{"Working-tree observation", label(string(p.Repository.WorkingTreeState))}, {"Working-tree limitation", p.Repository.WorkingTreeNote},
		{"Scan time (UTC)", scanTime.UTC().Format(time.RFC3339Nano)}, {"Tool", p.Tool.Name + " " + p.Tool.Version}, {"Rule set", p.Tool.RuleSetVersion},
		{"Packet schema", p.SchemaVersion}, {"Scan root (relative)", p.Boundary.Root},
		{"Entries visited", strconv.Itoa(p.Scan.EntriesVisited)}, {"Files considered", strconv.Itoa(p.Scan.FilesConsidered)},
		{"Maximum entries", strconv.Itoa(p.Boundary.Limits.MaxEntries)}, {"Maximum depth", strconv.Itoa(p.Boundary.Limits.MaxDepth)},
		{"Maximum file bytes", strconv.FormatInt(p.Boundary.Limits.MaxFileBytes, 10)},
		{"Maximum total read bytes", strconv.FormatInt(p.Boundary.Limits.MaxTotalReadBytes, 10)},
		{"Evidence digest (SHA-256)", p.Integrity.Digest},
	}
}

func label(value interface{}) string { return strings.ReplaceAll(fmt.Sprint(value), "_", " ") }

func markdownValue(input interface{}) string {
	value := fmt.Sprint(input)
	if value == "" {
		return "(not recorded)"
	}
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	value = strings.ReplaceAll(value, "\n", " ⏎ ")
	longest, run := 0, 0
	for _, char := range value {
		if char == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	delimiter := strings.Repeat("`", longest+1)
	return delimiter + " " + value + " " + delimiter
}
