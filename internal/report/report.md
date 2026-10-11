# Release Evidence report

Experimental, offline record for {{value .Packet.Repository.Name}}.

This report does not certify security, compliance, or release readiness. A recorded approval is a human assertion; the tool does not verify the person's authority.

## Decision record

{{if not .Packet.Scan.Complete}}**Incomplete scan.** Some evidence could not be inspected. Missing results do not establish absence.
{{end}}
{{range .Counts}}- {{.Label}}: {{.Total}}
{{end}}
Counts describe observations, not a readiness or security score.

- Human decision: {{value (label .Packet.Decision.Status)}}
- Decision owner: {{value .Packet.Decision.Owner}}
- Decision rationale: {{value .Packet.Decision.Rationale}}
- Decision updated: {{value .Packet.Decision.UpdatedAt}}
- Human assessment: {{value (label .Packet.Assessment.Status)}}
- Review context: {{value .Packet.Assessment.Context}}
- Assessment updated: {{value .Packet.Assessment.UpdatedAt}}
{{if .Comparison}}
## Changes between packets

Rules are matched by their stable identifier. File contents are not compared; citations record paths and line numbers. Confirm that these packets describe the intended repository.

{{range .Comparison.Warnings}}- {{.}}
{{end}}
{{len .Comparison.Changes}} added, removed, or changed rules; {{.Comparison.Unchanged}} unchanged rules.

### Previous packet metadata

{{range .BeforeMetadata}}- {{.Label}}: {{value .Value}}
{{end}}- Scan complete: {{.Comparison.Before.Scan.Complete}}
{{range .Comparison.Changes}}
### {{.Kind}} rule: {{value .ID}}

{{if .Fields}}Changed fields: {{range .Fields}}{{value .}} {{end}}
{{end}}
Previous observation:
{{if .Before}}{{template "machine" .Before}}{{else}}
Rule not present in the previous packet.
{{end}}
Current observation:
{{if .After}}{{template "machine" .After}}{{else}}
Rule not present in the current packet. Removal does not establish that evidence disappeared.
{{end}}
{{else}}
No machine-observation changes were recorded.
{{end}}
### Human assessment changes
{{if .Comparison.ContextChanged}}
- Previous status: {{value (label .Comparison.AssessmentBefore.Status)}}
- Previous context: {{value .Comparison.AssessmentBefore.Context}}
- Current status: {{value (label .Comparison.AssessmentAfter.Status)}}
- Current context: {{value .Comparison.AssessmentAfter.Context}}
{{end}}
{{range .Comparison.AssessmentChanges}}
Finding: {{value .FindingID}}

Previous human assessment:
{{if .Before}}{{template "assessment" .Before}}{{else}}
Not recorded.
{{end}}
Current human assessment:
{{if .After}}{{template "assessment" .After}}{{else}}
Not recorded.
{{end}}
{{else}}
No per-finding assessment changes were recorded.
{{end}}
{{if .Comparison.DecisionChanged}}
### Human decision changed

Previous decision:
{{template "decision" .Comparison.DecisionBefore}}
Current decision:
{{template "decision" .Comparison.DecisionAfter}}
{{else}}
The recorded human decision is unchanged.
{{end}}
### Current unknown or not-observed evidence

These are machine observations. A human disposition does not change the scanner's result.

{{range .Comparison.Unresolved}}- {{value .}}
{{else}}- None in the current packet.
{{end}}{{end}}
## Recorded evidence

Observed means supported evidence was found. Not observed means the supported search did not find it. Unknown means the tool could not decide. Not applicable is a human judgment. These states do not establish that a practice is effective.
{{range .Findings}}
### {{value .Title}}

- Category: {{value .Category}}
- Rule: {{value .ID}}
{{template "machine" .Finding}}
Human assessment:
{{if .HasHuman}}{{template "assessment" .Human}}{{else}}
No assessment recorded. This item remains open for human review.
{{end}}
{{else}}
No findings in this packet.
{{end}}
## Open review items

Items without an assessment, or with an open or assigned disposition, need human follow-up. Accepted, resolved, and not-applicable dispositions record human judgments.

{{range .OpenItems}}- {{value .Title}} ({{value .ID}}){{if .HasHuman}}: {{value (label .Human.Disposition)}}; owner {{value .Human.Owner}}; target {{value .Human.TargetDate}}; rationale or next action {{value .Human.Rationale}}{{else}}: no assessment recorded{{end}}
{{else}}- No open review items recorded. This does not establish readiness.
{{end}}
## Scan boundary and provenance

{{range .Metadata}}- {{.Label}}: {{value .Value}}
{{end}}- Scan complete: {{.Packet.Scan.Complete}}

The digest covers normalized evidence, not the human assessment or decision. It is reproducibility metadata, not a signature or attestation. This export reads the saved packet; it does not verify current repository contents.

### Active exclusions

{{range .Packet.Boundary.Exclusions}}- {{value .}}
{{else}}- No exclusions recorded.
{{end}}
### Scan diagnostics

{{range .Packet.Diagnostics}}- {{value .Severity}} / {{value .Code}} / {{value .Path}}: {{value .Message}}
{{else}}- No diagnostics recorded.
{{end}}
Review filenames, owner labels, and free-text notes before sharing this report. No source excerpts or absolute scan root are added by the exporter; free-text notes may still contain private information. In code spans, the marker ⏎ represents a line break in the saved text.
{{define "machine"}}
- Scanner observation: {{value (label .State)}}
- Rule version: {{value .RuleVersion}}
- Observation confidence (not software safety): {{value .Confidence}}
- Explanation: {{value .Explanation}}
- Detector limitations: {{value .Limitations}}
- Citations:{{range .Citations}} {{value .Path}}{{if .Line}} (line {{.Line}}){{end}};{{else}} none{{end}}
- Search boundary:{{range .SearchBoundary}} {{value .}};{{end}}
{{end}}
{{define "assessment"}}
- Human disposition: {{value (label .Disposition)}}
- Rationale or next action: {{value .Rationale}}
- Owner: {{value .Owner}}
- Target date: {{value .TargetDate}}
{{end}}
{{define "decision"}}
- Status: {{value (label .Status)}}
- Owner: {{value .Owner}}
- Rationale: {{value .Rationale}}
{{end}}
