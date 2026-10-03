# Contributing

Release Evidence is an experimental project. Focused bug reports, usability
observations, documentation corrections, and small pull requests are welcome.

Before proposing a substantial feature, open an issue so its effect on the
product boundary can be discussed. In particular, network access, repository
credentials, executable rules, vulnerability claims, and automatic release
decisions require explicit design and security review.

## Development checks

From the repository root:

```sh
go test ./...
go vet ./...
```

To rebuild and test the embedded interface:

```sh
cd ui
pnpm install --frozen-lockfile
pnpm build
pnpm exec playwright install chromium
pnpm test:e2e
```

Keep changes small, add or update tests for changed behavior, and preserve the
distinction between machine observations and human judgments. Repository
fixtures must be synthetic and must not contain real secrets or customer data.

Unless explicitly stated otherwise, intentionally submitted contributions are
licensed under the Apache License 2.0, as described in section 5 of the license.
See [LICENSE](LICENSE).
