# Changelog

All notable changes to this project will be documented in this file.

## [2.0.1] - 2026-10-19

### Bug Fixes
- Object query parameters are now sent to NetXMS as `inputFields` (previously silently ignored by the server)
- Query editor runs the query right after the last required field is selected
- Errors from NetXMS while loading dropdown lists (e.g. invalid API key) are shown in the query editor instead of leaving the lists empty
- Summary table / object query columns with numeric, boolean or null cells no longer crash the plugin
- DCI history points with a null timestamp are skipped instead of failing the whole series
- Alarm Id and Count are decoded as unsigned 32-bit integers, matching the server

### Changes
- Upgrade Go toolchain to 1.26, Grafana Go plugin SDK to v0.297.0 and grpc to 1.84 (fixes CVE-2026-33186)
- Refresh npm lockfile to fix vulnerable transitive dependencies reported by the Grafana plugin validator
- Release workflow: build, sign, package and validate in a reusable workflow; the plugin validator blocks the release, unfixable vulnerabilities are excepted with an expiry date
- Add weekly security scan workflow running the full plugin validator
- CI: update GitHub Actions to Node 24 releases, skip e2e for Dependabot PRs, fix create-plugin update workflow (default branch instead of `main`)
- Add Dependabot configuration for npm, Go modules and GitHub Actions

## [2.0.0] - 2026-01-15

### Features
- Initial release of NetXMS Grafana data source plugin
- Query alarms, DCI values, summary tables, object status, and object queries from NetXMS
- Health check endpoint for diagnostics
- Secure API key authentication

### Bug Fixes
- N/A (initial release)

### Breaking Changes
- N/A (initial release)

---
