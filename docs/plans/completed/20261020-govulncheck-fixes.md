# Fix govulncheck findings in the Go backend

## Overview

`govulncheck ./...` reports reachable vulnerabilities in the backend. With the local Go 1.27.3 toolchain only `golang.org/x/net` shows up; with the toolchain `go.mod` declares (`go 1.26.5`) the standard library adds twelve more. Fix both: bump `golang.org/x/net` and raise the `go` directive to the first patch release that fixes the stdlib findings.

## Context (from discovery)

- `golang.org/x/net v0.59.0 // indirect` (via grafana-plugin-sdk-go): GO-2026-6617, -6612, -6611, -6610, -6603, all fixed in v0.60.0. Reachable through `datasource.Manage` and `CheckHealth`'s `http.Client.Do`.
- Stdlib at go1.26.5: GO-2026-6617, -6613, -6612, -6611, -6610, -6608, -6607, -6605, -6603, -6600, -6599 (fixed in 1.26.9) and GO-2026-6218, -6091, -6090, -6089, -5972, -5026 (fixed in 1.26.6).
- CI (`ci.yml`, `build-plugin.yml`) uses `setup-go` with `go-version: '1.26'`, so it likely already builds with the latest 1.26.x; the `go` directive is what pins the minimum for every other build.
- `.github/osv-scanner-validator.toml` has no Go entries; nothing to remove there.

## Decisions

- Raise the `go` directive to `1.26.9` rather than add a `toolchain` line: this is a plugin binary, not a library, so a higher minimum costs no consumer anything.
- Bump only `golang.org/x/net`; no unrelated module upgrades (Dependabot covers those).

## Tandem

| Task | Writer | Reviewer |
|---|---|---|
| 1 | claude | codex |

## Implementation Steps

### Task 1: Bump golang.org/x/net and the Go directive

Files: `go.mod`, `go.sum`, `CHANGELOG.md`.

- [x] `golang.org/x/net` at v0.60.0 (`go get golang.org/x/net@v0.60.0 && go mod tidy`)
- [x] `go` directive is `1.26.9`
- [x] `GOTOOLCHAIN=go1.26.9 govulncheck ./...` reports no vulnerabilities
- [x] `CHANGELOG.md` `[Unreleased]` / Changes entry naming the x/net bump and the Go 1.26.9 minimum with the GO-2026 IDs summarised
- [x] `mage coverage` passes and `golangci-lint run ./...` is clean
- [x] one `fix:` commit

## Post-Completion

- After merge, confirm the weekly security scan / plugin validator run is green.
