# Release Evidence

[![CI](https://github.com/drimini-software/release-evidence/actions/workflows/ci.yml/badge.svg)](https://github.com/drimini-software/release-evidence/actions/workflows/ci.yml)

> Experimental preview. The workflow is working, but its usefulness has not
> yet been validated with independent target users.

Release Evidence is a local-first workbench for reviewing the repository facts
and human decisions behind a software release or handover.

It scans one local directory for a deliberately small set of evidence, including
build and dependency definitions, ownership, tests, continuous integration,
deployment, recovery, security reporting, and runbook material. A loopback-only
review interface lets a person record applicability, ownership, follow-up
actions, and the decision context alongside those observations.

Release Evidence does not upload repository data, execute repository content,
or automatically decide whether a release is safe.

## Try it

The current preview is run from source and requires Go 1.20 or newer:

```sh
git clone https://github.com/drimini-software/release-evidence.git
cd release-evidence

go run ./cmd/release-evidence scan --output packet.json /path/to/repository
go run ./cmd/release-evidence review /path/to/repository
```

`scan` writes a versioned JSON packet. Use `-` as the output path, or omit
`--output`, to write JSON to standard output.

`review` starts a temporary local interface on `127.0.0.1`. Review state is
saved atomically outside the scanned repository by default. Pass
`--state /path/to/packet.json` when you want an explicit location.

Multiple tabs of one review session cannot silently replace each other's newer
saved changes. A stale save keeps the tab's edits visible and blocks saving and
downloads. Copy the pending edits before reloading, inspect the latest saved
review, then reapply the changes you still want. Changes are not merged or
overwritten automatically. A save whose success response was lost can also
require this recovery. This protection applies within one running review
process; it does not lock a state file shared by separate processes.

Use **Keep a copy** on the review page to download HTML, Markdown, or the
complete JSON packet. Save any edits first; downloads include the current
session's observations and recorded judgments without another scan. Editing
pauses while saving or preparing a download. Downloading does not save review
state, and the browser chooses the destination.

Use **See what changed** to select an earlier saved JSON packet (up to 4 MiB)
and download a comparison with the current review as HTML, Markdown, or JSON.
Save edits first and confirm that the selected packet describes the intended
repository. Open the downloaded comparison to inspect changes and warnings.
The earlier file is processed by the local session for that response; it is
not saved as review history. Review confidential text from both packets before
sharing. A JSON comparison is a change record, not an input packet for another
comparison.

## Portable reports and comparisons

Export a saved scan or review packet without access to its repository:

```sh
go run ./cmd/release-evidence export --output .cache/report.html packet.json
go run ./cmd/release-evidence export --format markdown --output .cache/report.md packet.json
```

HTML reports are self-contained, script-free, and printable. Markdown keeps
repository and review text in code spans so it cannot introduce links, images,
or active HTML. Both formats include the recorded decision, findings, citations,
limitations, open review items, exclusions, diagnostics, and evidence provenance.
Review filenames, owner labels, and free-text notes before sharing an export.
The exporter does not add source excerpts or an absolute scan root; notes
entered by a person can still contain sensitive information.

Compare two saved packets, placing the earlier packet first:

```sh
go run ./cmd/release-evidence compare --output .cache/changes.html previous.json current.json
go run ./cmd/release-evidence compare --format markdown --output .cache/changes.md previous.json current.json
go run ./cmd/release-evidence compare --format json previous.json current.json
```

Comparison matches rules by their stable identifier and distinguishes added,
removed, changed, and unchanged observations. Human assessment and decision
changes are listed separately, alongside current unknown or not-observed
evidence. Timestamps and collection ordering do not count as evidence changes.
Different detector versions, scan boundaries, diagnostics, incomplete scans,
or unknown revision/working-tree state are called out explicitly.

Confirm that both packets describe the intended repository. Comparison checks
recorded observations and citation locations, not file contents or deployed
systems. Removing a rule does not establish that its evidence disappeared.
The evidence digest is reproducibility metadata, not a signature; it does not
cover human judgments or establish who entered them.

`export` defaults to HTML; `compare` also supports Markdown and JSON. Omit
`--output`, or use `--output -`, to write the result to standard output.
Files are replaced only after successful rendering and flushing. An output
cannot replace either input packet, and links or directories are rejected as
output targets. Use `.cache/` for local reports to keep them out of this source
repository. Reports remain experimental evidence records, not release approval.

Imported packets must use the schema's exact lowercase field names and unique
members in every JSON object. Duplicate fields (including escaped spellings),
case aliases, invalid UTF-8 bytes, unpaired Unicode surrogate escapes, and
nesting beyond 32 levels are rejected before schema and evidence-digest checks.
The existing 4 MiB input limit applies to CLI and review-page imports. Ordinary
Unicode text and valid escaped field names are preserved. These checks remove
ambiguous representations; they do not authenticate recorded human decisions.

## What the results mean

Each detector produces one of these machine-observation states:

- **Observed:** matching evidence was found at a cited local path.
- **Not observed:** the supported search completed without finding matching
  evidence. This is not proof that the evidence does not exist elsewhere.
- **Unknown:** the tool could not responsibly decide because input was
  unsupported, ambiguous, unreadable, or outside a resource limit.
- **Not applicable:** a human marked the item outside this decision's scope and
  supplied their own judgment.

Finding evidence does not establish that a practice is effective or that the
system is secure. Release Evidence is not a vulnerability scanner, compliance
assessment, penetration test, certification, or release-approval authority.

## Privacy and security boundary

The default scan is deterministic, telemetry-free, and offline. The scanner
does not execute hooks, builds, tests, templates, or other repository-provided
code. Repository paths are inspected through bounded readers and escaping links
are not followed.

The review interface is served by the same executable on a temporary loopback
port. It uses a per-run capability token and validates state-changing requests.
See [architecture.md](architecture.md) for the current design and limitations.

Please report suspected vulnerabilities according to
[SECURITY.md](SECURITY.md), not in a public issue.

## Development

The preferred development toolchain is pinned in `.go-version` and
`ui/package.json`. The Go module currently preserves Go 1.20 language
compatibility.

```sh
go test ./...
go vet ./...
go test -race ./internal/review

# Optional bounded fuzz runs; CI runs both targets for 20 seconds each.
go test ./internal/packet -run '^$' -fuzz '^FuzzDecodePacket$' -fuzztime 20s -parallel 2
go test ./internal/packet -run '^$' -fuzz '^FuzzPacketText$' -fuzztime 20s -parallel 2

cd ui
pnpm install --frozen-lockfile
pnpm build
pnpm exec playwright install chromium
pnpm test:e2e
```

The TypeScript build writes the interface bundle embedded by the Go executable.
Synthetic scanner inputs live in `fixtures/`, and the packet contract is in
`schema/packet.schema.json`.

The race-detector check needs a supported platform and an available C compiler.
CI runs it on Ubuntu. See Go's
[race-detector documentation](https://go.dev/doc/articles/race_detector.html).

Ordinary `go test ./...` also runs the fuzz seed cases. The two bounded CI runs
exercise damaged packet imports and valid hostile text through encoding,
comparison, and report rendering. On a discovered failure, Go writes a minimal
reproducer under `internal/packet/testdata/fuzz/`; CI retains that directory in
a run-specific artifact for seven days. Reproduce it locally, fix the fault,
and retain the input as a regression test. Fuzzing is sampled coverage, not a
proof that all hostile inputs are safe. See Go's
[fuzzing documentation](https://go.dev/doc/security/fuzz/).

Contributions are welcome within the project's narrow safety boundary. Read
[CONTRIBUTING.md](CONTRIBUTING.md) before proposing substantial functionality.

## Project status

This repository is public to make the experiment inspectable and to find people
for whom the workflow is genuinely useful. It has no stable release,
supported binary, service-level commitment, certification claim, or commercial
availability. The name is provisional.

## License

Licensed under the [Apache License 2.0](LICENSE).
