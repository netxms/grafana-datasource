# Upgrade Go 1.26, Fix CVEs, Update Yarn and Dependencies

## Overview

Upgrade the Go toolchain to 1.26, upgrade all Go and Node.js packages to resolve CVE findings from osv-scanner, fix any lint errors introduced by the upgrades, and align Yarn version across CI and local config.

## Context

- Files involved:
  - `go.mod`, `go.sum` — Go module definition and checksums
  - `package.json`, `yarn.lock` — Node.js dependencies and lockfile
  - `.github/workflows/ci.yml` — CI workflow (Go version, Yarn version)
  - `.github/workflows/release.yml` — Release workflow (Go version)
  - `.golangci.yml` — Linter configuration
  - `pkg/plugin/datasource.go` — Main backend source (potential lint fixes)
  - `pkg/plugin/datasource_test.go` — Backend tests
  - `pkg/models/settings.go` — Backend models
  - `pkg/main.go` — Backend entry point
- CVEs to resolve:
  - CRITICAL: CVE-2026-33186 in `google.golang.org/grpc` v1.78.0
  - HIGH: CVE-2026-32141, CVE-2026-33228 in `flatted` (3.3.3/3.3.4)
  - HIGH: CVE-2026-31802, CVE-2026-29786 in `tar` (7.5.9)

## Development Approach

- **Testing approach**: Regular (upgrade, then verify tests pass)
- Complete each task fully before moving to the next
- **CRITICAL: all tests must pass before starting next task**

## Implementation Steps

### Task 1: Upgrade Go toolchain to 1.26

**Files:**
- Modify: `go.mod`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`

- [x] Update `go.mod` directive from `go 1.25.7` to `go 1.26`
- [x] Update `.github/workflows/ci.yml` go-version from `'1.25'` to `'1.26'` (line 66)
- [x] Update `.github/workflows/release.yml` go-version from `'1.25'` to `'1.26'` (line 23)
- [x] Run `go mod tidy` to clean up module files

### Task 2: Upgrade Go dependencies to fix grpc CVE

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [x] Run `go get google.golang.org/grpc@latest` to upgrade grpc beyond v1.78.0 (fixes CVE-2026-33186)
- [x] Run `go get github.com/grafana/grafana-plugin-sdk-go@latest` to pull in latest Grafana plugin SDK
- [x] Run `go get -u ./...` to upgrade all direct and indirect dependencies
- [x] Run `go mod tidy` to clean up unused dependencies
- [x] Verify the build compiles: `go build ./...`
- [x] Run Go tests: `go test ./...`

### Task 3: Fix Go lint errors

**Files:**
- Modify: `pkg/plugin/datasource.go` (if needed)
- Modify: `pkg/models/settings.go` (if needed)
- Modify: `pkg/main.go` (if needed)
- Modify: `.golangci.yml` (if needed)

- [x] Run `golangci-lint run ./...` and capture output
- [x] Fix any lint errors found (new errors from Go 1.26 or updated linters)
- [x] Re-run `golangci-lint run ./...` to confirm all lint errors are resolved
- [x] Run Go tests again: `go test ./...`

### Task 4: Upgrade Node.js dependencies to fix flatted and tar CVEs

**Files:**
- Modify: `package.json`
- Modify: `yarn.lock`

- [x] Add resolution for `flatted` to latest patched version in `package.json` resolutions (fixes CVE-2026-32141, CVE-2026-33228)
- [x] Update existing `tar` resolution to latest patched version (fixes CVE-2026-31802, CVE-2026-29786)
- [x] Run `yarn up` for all Grafana packages (`@grafana/data`, `@grafana/runtime`, `@grafana/ui`, `@grafana/schema`, `@grafana/i18n`, `@grafana/eslint-config`, `@grafana/plugin-e2e`, `@grafana/tsconfig`) to latest compatible versions
- [x] Run `yarn up` for other outdated devDependencies (eslint ecosystem, playwright, typescript, webpack, etc.)
- [x] Run `yarn install` to regenerate lockfile
- [x] Run frontend tests: `yarn test:ci`
- [x] Run typecheck: `yarn typecheck`

### Task 5: Align Yarn version in CI

**Files:**
- Modify: `.github/workflows/ci.yml`

- [x] Update `corepack prepare yarn@4.9.1` to `corepack prepare yarn@4.9.2` in CI build job (line 40)
- [x] Update `corepack prepare yarn@4.9.1` to `corepack prepare yarn@4.9.2` in CI playwright-tests job (line 186)

### Task 6: Verify acceptance criteria

- [x] Run full Go test suite: `mage coverage`
- [x] Run full frontend test suite: `yarn test:ci`
- [x] Run linter: `yarn lint`
- [x] Run Go linter: `golangci-lint run ./...`
- [x] Run typecheck: `yarn typecheck`
- [x] Build frontend: `yarn build`
- [x] Build backend: `mage buildAll`
- [x] Verify `google.golang.org/grpc` version in go.mod is >= v1.79.0
- [x] Verify `flatted` and `tar` in yarn.lock are at patched versions

### Task 7: Update documentation

- [x] Update CLAUDE.md if Go version reference or build commands changed (no changes needed - no Go version references in CLAUDE.md)
- [x] Move this plan to `docs/plans/completed/`
