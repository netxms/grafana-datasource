# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Breaking Changes
- Require NetXMS 7.0.0 or later (was 5.2.4). DCI history relies on server-side aggregation of the history endpoint (#24)
- Require Grafana 12.4.0 or later (was 10.4.0), the oldest Grafana line still [supported upstream](https://grafana.com/docs/grafana/latest/upgrade-guide/when-to-upgrade/). The query type selector uses `Combobox`, which is unavailable before Grafana 11.5; on Grafana 10.4–11.4 the query type selector did not render (#26)

### Bug Fixes
- Numeric DCI history passes the query's max data points to NetXMS, which averages raw samples into roughly that many buckets (timestamps are bucket starts). When NetXMS reads raw samples, a long range of a numeric DCI no longer loses its beginning to the server's 200000-row limit, and the browser receives about one point per pixel instead of every sample. When NetXMS aggregation is enabled the server may answer from hourly or daily aggregates instead, which can be coarser than the requested resolution (#24)
- DCI history served from NetXMS hourly/daily aggregates (when `DataCollection.Aggregation.Enabled` is on) is shown, instead of an empty string series. Hourly/daily series carry their step as the field interval, so missing hours or days show as gaps (#24)
- A warning notice is shown when NetXMS returns its 200000-row limit for a DCI series, which can still happen for string DCIs, queries without max data points, and very long hourly/daily ranges (#24)
- Fix the e2e matrix, which did not pass the Grafana version to the container and ran the Compose default instead of resolved versions. Stable-version jobs now check the running version; the earlier 2.0.1 claim of cross-version e2e coverage was incorrect
- DCI history is returned oldest-first, so Stat / Gauge / Bar gauge `Last` and `First` reducers show the correct sample (previously reversed) (#23)
- Object query columns are taken from all rows, so a field set only for some objects is no longer dropped when the first object lacks it (#25)

### Changes
- `docker compose up` starts a NetXMS test server (`ghcr.io/netxms/server-e2e:7.0.0-e2e.1`) next to Grafana, with the provisioned data source pointed at it, so the development environment works without configuration (#21)
- Update `golang.org/x/net` to v0.60.0 (fixes HTTP/2 vulnerabilities GO-2026-6603, -6610, -6611, -6612, -6617)
- Require Go 1.26.9 to build the backend (fixes standard library vulnerabilities in `net/http`, `net/url`, `net/textproto`, `crypto/tls`, `encoding/asn1` and `html/template` reported by govulncheck)

## [2.0.1] - 2026-10-20

### Bug Fixes
- Object query parameters are now sent to NetXMS as `inputFields` (previously silently ignored by the server)
- Query editor runs the query right after the last required field is selected
- Errors from NetXMS while loading dropdown lists (e.g. invalid API key) are shown in the query editor instead of leaving the lists empty
- Summary table / object query columns with numeric, boolean or null cells no longer crash the plugin
- DCI history points with a null timestamp are skipped instead of failing the whole series
- Alarm Id and Count are decoded as unsigned 32-bit integers, matching the server
- NetXMS errors in dropdown requests are returned as 502 instead of 401, so Grafana no longer treats them as an expired Grafana session
- Config editor shows a correct server address example (no `/api` path)

### Changes
- Build against Grafana 13 frontend packages (`@grafana/data`, `@grafana/ui`, `@grafana/runtime`, ...); Grafana 10.4+ is still supported and covered by e2e tests
- Update build and test tooling (webpack, SWC, Playwright, ESLint plugins and others)
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
