# Release Evidence

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

cd ui
pnpm install --frozen-lockfile
pnpm build
pnpm exec playwright install chromium
pnpm test:e2e
```

The TypeScript build writes the interface bundle embedded by the Go executable.
Synthetic scanner inputs live in `fixtures/`, and the packet contract is in
`schema/packet.schema.json`.

Contributions are welcome within the project's narrow safety boundary. Read
[CONTRIBUTING.md](CONTRIBUTING.md) before proposing substantial functionality.

## Project status

This repository is being published to make the experiment inspectable and to
find people for whom the workflow is genuinely useful. It has no stable release,
supported binary, service-level commitment, certification claim, or commercial
availability. The name is provisional.

## License

Licensed under the [Apache License 2.0](LICENSE).
