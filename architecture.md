# Release Evidence architecture

Status: Implemented direction for scanner and local review slices
Last reviewed: 2026-09-27

## Architectural goal

Produce a trustworthy, inspectable evidence packet from one local repository
without executing repository content, requiring an account, or sending source
material over the network.

The architecture optimizes for a complete local journey and a small team's
ability to understand it. It does not anticipate a hosted platform.

## System context

```text
                     read-only, bounded
┌──────────┐       ┌────────────────────┐
│ Operator │──────▶│ Local Go process   │──────▶ Repository
└────┬─────┘       │                    │
     │             │ scanner/detectors  │
     │ browser     │ evidence model     │
     ▼             │ assessment store   │
┌──────────┐       │ report exporter    │
│ Local UI │◀─────▶│ loopback API       │
└──────────┘       └─────────┬──────────┘
                             │ atomic local writes
                             ▼
                    Packet JSON / HTML / Markdown
```

There is no hosted service or external API in the first slice. The local UI is
served by the same executable on a random loopback port and uses embedded,
versioned assets.

## Technology choices

### Go executable

Use Go for filesystem traversal, detectors, the local HTTP process, export, and
packaging.

Reasons:

- straightforward cross-platform single-binary distribution;
- a strong standard library for paths, structured data, HTTP, cancellation, and
  testing;
- simple concurrency where independent detectors benefit from it;
- appropriate fit with Drimini's preferred technology territory;
- easier operational handover than a multi-service runtime.

The first command surface should use the standard library unless a dependency
earns its cost. Do not add a plugin runtime, embedded scripting language, or
general rule DSL in the first slice.

### Strict TypeScript interface

Use strict TypeScript, semantic HTML, and native CSS for the local review
interface. A small build tool may bundle the interface into static assets, then
Go's embed support packages them into the executable.

Start without a runtime UI framework. Adopt one only if measured interaction
complexity makes the state and accessibility behavior clearer, not because a
framework is customary.

### Versioned JSON storage

Use versioned JSON documents for scan results and human assessment. Do not add a
database in the first slice.

Reasons:

- packets remain inspectable and portable;
- golden and compatibility tests are simple;
- export and change comparison operate on the same canonical model;
- the product can prove the workflow before accepting database migrations or a
  hosted data model.

Writes use create-temporary, flush, close, and atomic replace behavior supported
by the target platform. Preserve the last valid version if replacement fails.

## Runtime modes

### `release-evidence scan <repository>`

Runs a non-interactive scan and writes normalized JSON to an explicit output or
standard output. It does not start a browser or accept manual judgments.

### `release-evidence review <repository>`

Scans the repository, starts a loopback-only review server on an operating
system-assigned port, opens the local interface when allowed, and persists human
assessment separately from scanner output.

### `release-evidence export <packet>`

Produces a self-contained inert HTML report and a Markdown summary from an
existing packet. Export never needs repository access.

Additional commands require evidence. Do not add daemon, account, cloud, or
organization modes in the first slice.

## Component boundaries

### 1. Command layer

- validates explicit paths and options;
- establishes cancellation and resource budgets;
- chooses scan, review, or export mode;
- communicates failures without stack traces or sensitive values by default.

It contains no detector logic.

### 2. Repository boundary

Creates the only interface detectors may use to inspect a repository. It:

- resolves and records the selected root;
- enumerates entries using non-following metadata operations;
- applies ignore and exclusion rules;
- enforces path, depth, file-count, per-file, and aggregate-read limits;
- provides bounded text and structured-data readers;
- records unreadable, skipped, and unsupported input as scan diagnostics;
- never exposes an execute primitive.

Detectors must not call unrestricted filesystem APIs against repository paths.
Tests should enforce this boundary through package structure and review.

### 3. Detector engine

Runs versioned deterministic rules against the repository boundary. The initial
engine supports built-in detectors only.

Each detector declares:

- stable rule ID and version;
- category and purpose;
- paths and file types it may inspect;
- read budget;
- possible observed evidence;
- conditions that yield not observed or unknown;
- limitations displayed to the user.

Detector failures become scoped diagnostics and unknown results where relevant.
One malformed file must not erase otherwise valid evidence.

### 4. Evidence normalizer

Validates detector output, rejects invalid citations, normalizes relative paths,
sorts unordered collections, separates diagnostics, and produces the canonical
scan document.

The normalizer is the only component allowed to produce a serializable scan
packet. This provides one place to enforce determinism and schema validity.

### 5. Assessment service

Maintains human-provided context separately from scan facts:

- decision title and type;
- applicability decisions;
- rationale and risk acceptance;
- action and owner label;
- target date;
- overall release or handover decision;
- author label and timestamps.

The tool records what a user entered; it does not validate their authority to
accept risk. The report must say so.

### 6. Local review server

Serves embedded UI assets and a narrow JSON API. It must:

- bind only to an explicit loopback address, never `0.0.0.0` or a LAN address;
- request a random operating-system port;
- generate a cryptographically random per-run capability token;
- expose the token only through the same-origin bootstrap document;
- require the token on state-changing API calls;
- validate `Host`, `Origin`, method, content type, and request size;
- reject cross-origin requests and avoid permissive CORS headers;
- set a restrictive Content Security Policy;
- time out idle sessions and stop cleanly;
- never serve arbitrary repository files by URL.

The loopback server is still a security boundary. Binding locally does not make
request validation optional.

### 7. Interface

The interface receives normalized facts and assessment data, never arbitrary
HTML. It renders repository-controlled strings as text.

Primary views:

- decision overview;
- findings grouped by category and state;
- finding explanation, citations, and limitations;
- unanswered human questions;
- owners and residual actions;
- changes from a prior packet;
- export preview.

Color is secondary to text and icon labels. Every interaction works by keyboard
and exposes an accessible name, state, and error.

### 8. Exporter

Transforms only validated packet data into report formats. It:

- escapes all untrusted strings;
- does not embed scripts in the portable HTML report;
- avoids absolute local paths by default;
- includes schema, tool, rule-set, and repository-revision context;
- lists exclusions, incomplete scans, limitations, and unresolved unknowns;
- distinguishes observed facts from manual assertions;
- never emits an overall certification or security score.

## Evidence state semantics

### Observed

The detector found evidence matching its documented rule within the actual scan
boundary. This says the evidence exists at the cited location; it does not say
the practice is effective or the system is safe.

### Not observed

The detector completed its supported search and found no matching evidence. It
must list the search boundary. This is not proof that the evidence does not
exist elsewhere.

### Unknown

The tool cannot responsibly decide. Causes include unsupported formats,
ambiguous configuration, read failures, exceeded budgets, or a question that
requires operational context.

### Not applicable

A human deliberately marked the item outside this decision's context and
provided a rationale. Detectors never assign this state.

## Canonical document shape

The first schema is committed at `schema/packet.schema.json`. Its top-level
design is:

```json
{
  "schema_version": "1",
  "tool": {},
  "repository": {},
  "boundary": {},
  "scan": {},
  "findings": [],
  "diagnostics": [],
  "assessment": {},
  "decision": {},
  "integrity": {}
}
```

Key rules:

- Scanner facts and assessment assertions remain separate objects.
- Repository paths are slash-normalized and relative to the selected root.
- Findings use stable IDs independent of display order.
- Timestamps are excluded from deterministic content comparison.
- Integrity metadata may include a digest of the normalized packet; it is not a
  cryptographic attestation unless explicitly signed later.
- New fields are additive within a schema version; breaking changes require a
  migration and a new version.

## Initial detector families

The first detectors recognize evidence; they do not evaluate its real-world
effectiveness.

### Repository context

- Git revision when available;
- clean, dirty, or unknown working-tree state;
- active scan exclusions and diagnostics.

### Ownership

- supported ownership metadata such as CODEOWNERS or explicit product metadata;
- maintainership and security contact documents;
- human question for actual operational owner and decision authority.

### Build and dependencies

- recognized package manifests and lockfiles;
- documented build command or supported build metadata;
- presence of SBOM or provenance artifacts without claiming completeness or
  validity beyond implemented checks.

### Verification

- supported test configuration and test directories;
- supported continuous-integration workflow definitions;
- human question for last successful relevant run and required checks.

### Deploy and operate

- supported deployment configuration;
- deployment, rollback, recovery, backup, incident, runbook, and monitoring
  documentation;
- human questions for external systems and operational verification that a
  repository cannot prove.

Keep the rule set deliberately small. Each new rule needs a user decision it
supports, a documented limit, fixtures, and a false-assurance review.

## Repository threat model

Assume the selected repository may be accidental or deliberately malicious.

| Threat | Initial control |
| --- | --- |
| Symlink, junction, or reparse-point escape | Do not traverse links; verify every opened path remains under the resolved root |
| Huge or deeply nested tree | Depth, entry-count, per-file, aggregate-byte, and time budgets with cancellation |
| Device, socket, or special file | Read regular files only |
| Malformed JSON/YAML/TOML/XML | Bounded parsers, panic recovery at detector boundary, scoped unknown result |
| Billion-laughs or parser expansion | Disable external entities and unsafe features; reject unsupported constructs |
| HTML/script injection through names or values | Treat all strings as text; central escaping; script-free exports; CSP in local UI |
| Secret leakage | Never seek secret values; redact sensitive patterns from diagnostics; omit source excerpts by default |
| Command or template execution | No repository-provided commands, hooks, templates, or code execution |
| Local-server request forgery | Loopback binding, per-run token, Host/Origin validation, narrow methods, no permissive CORS |
| Stale or substituted packet | Include revision, boundary, tool/rule versions, digest, and incomplete-scan diagnostics |
| Denial of service through many detectors | Shared budgets, bounded concurrency, cancellation, deterministic partial result |

A formal threat model should be added before external evaluation and revised
when any network, integration, archive, plugin, or AI feature is proposed.

## Privacy and data handling

- Default operation is offline and telemetry-free.
- Do not collect identifiers for analytics in the first slice.
- Store project state in an operating-system-appropriate local data directory,
  keyed without exposing the repository name in global logs. Let the user choose
  export paths.
- Export uses relative paths and no raw source excerpts by default.
- Show a review step before export because filenames, ownership labels, and
  repository structure may still be confidential.
- Logs contain detector IDs and safe diagnostics, not file contents, tokens,
  environment values, or absolute paths unless an explicit debug mode warns the
  user.
- No AI provider receives repository or packet data in the first slice.

## Failure behavior

- Invalid root: stop before creating a packet.
- Individual unreadable or malformed evidence source: continue within budgets,
  record a diagnostic, and use unknown where the failure affects a result.
- Budget exhausted: cancel remaining work, mark the scan incomplete, preserve
  completed findings, and make incompleteness prominent.
- Interface process ends: preserve the last valid packet; no background daemon
  remains.
- Export failure: leave the prior export intact and remove or clearly identify
  incomplete temporary output.
- Schema too new: refuse to mutate it and offer read-only metadata where safe.

Never silently convert an operational failure into “not observed.”

## Testing architecture

### Unit and contract tests

- repository boundary and path normalization;
- ignore and budget behavior;
- detector state semantics;
- citation validation;
- canonical ordering and serialization;
- schema migrations;
- output escaping and redaction.

### Golden fixtures

Keep small synthetic repositories that express:

- complete supported evidence;
- ordinary missing evidence;
- contradictory and misleading evidence;
- unsupported formats;
- dirty Git state;
- malicious names and markup;
- links and path escapes;
- oversized, deeply nested, and malformed data;
- fake secrets that must never appear in output.

Golden outputs should be reviewed like product copy because they encode the
meaning of each rule.

### Fuzz and property tests

Prioritize path handling, structured-data parsers, normalization, and exporter
escaping. Useful invariants include:

- no citation resolves outside the root;
- normalization is idempotent;
- serialization round-trips without changing facts;
- invalid input cannot panic the whole scan;
- repository text cannot become active report markup.

### Integration tests

- command to packet;
- review server token and origin controls;
- atomic save and interrupted-save recovery;
- packet-to-report export;
- change comparison;
- explicit no-network default path.

### Human checks

- keyboard and screen-reader review;
- plain-language understanding of the four states;
- visibility of incomplete scans and limitations;
- clean-machine install/uninstall and operating-system trust prompts.

## Build and release direction

- Pin Go, Node, and build dependencies in the repository.
- Build UI assets reproducibly before embedding them.
- Use continuous integration for tests and supported-platform packages.
- Generate checksums and build provenance.
- Sign distributable artifacts when the selected platform and release identity
  are ready.
- Publish a security reporting path and supported-version policy before broad
  distribution.

The source repository is public under the Apache License 2.0. Exact release
hosting, signing identity, update channel, and supported operating systems are
TBD and must be decided before publishing supported binaries.

## Architecture triggers requiring a new decision

Record a new decision before adding:

- network access in the default path;
- hosted storage, accounts, or authentication;
- repository-host or cloud credentials;
- plugins or executable user rules;
- archive extraction;
- vulnerability or compliance claims;
- AI model calls with packet or repository data;
- automatic changes, pull requests, deploys, or release gating;
- multi-repository or organization-wide state;
- a database or background service.
