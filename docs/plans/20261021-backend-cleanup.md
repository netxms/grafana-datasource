# Backend cleanup from the codebase review (#28)

## Overview

Issue #28 lists nine cleanup items from a full-codebase review. One remaining item changes what users see: dropdown requests that fail inside the plugin show only "Internal Server Error".

The rest is structure and latent bugs:
- the version comparison misreads pre-release suffixes, which can't change a result against today's minimum;
- the NetXMS HTTP round trip is copied into every handler;
- the severity colours are defined twice;
- table rows are decoded twice;
- scaffold leftovers;
- a test file named after bug history;
- an e2e test that asserts nothing.

The design was settled with the user and Codex. The work lands as eight commits directly on `main`, with no push.

## Context (from discovery)

- Drift since the review snapshot (95f286d):
  - Item 2 is done: `writeResourceError` / `parseErrorResponse` decode the body into a struct (#27).
  - Item 5, first half, is done: `handleTableQuery` has no "expected array" / "empty array" branches since the column-union change (#25).
  - Item 4 is already a backlog item: `docs/backlog/severity-colours-duplicate-status-table.md`.
- `NewDatasource` ignores its `DataSourceInstanceSettings` argument. Every handler calls `models.LoadPluginSettings` from `PluginContext` instead, and the query handlers do it inside the per-query loop.
- The SDK's `instanceProvider.NeedsUpdate` rebuilds the instance when the datasource's `Updated` timestamp or the Grafana config changes. Settings stored on the instance therefore never go stale.
- No `http.Client` sets a `Transport`. They already share `http.DefaultTransport` and its connection pool, so moving the client to the instance improves ownership but brings no pooling gain.
- `LoadPluginSettings` fails only on malformed or empty `jsonData`. Grafana stores it itself and sends at least `{}`.
- `QueryEditor.tsx` shows `data.reason || data.message || statusText || message`. A plain-text `http.Error` body has neither field, so it shows as "Internal Server Error".
- Request details the refactor must keep. No existing test covers them:
  - POSTs carry `Content-Type: application/json` and GETs carry none.
  - The alarm query sends a literal `{}` body when no root object is given.
  - A query's upstream 401 becomes `StatusUnauthorized` with `Unauthorized: Invalid API key`.
  - The `CheckHealth` messages are not asserted anywhere.
- NetXMS `/v1/server-info` `version` formats are `X.Y.Z`, `X.Y.Z-SNAPSHOT`, `X.Y.Z-rcN` and `X.Y.Z.N` (service pack), produced by `build/updatetag.pl` in netxms-master.
  - `strconv.Atoi("0-rc2")` gives 0, so `7.0.0-rc2` already passes a `7.0.0` minimum.
  - The bug shows only with a nonzero part before the suffix: `7.0.4-rc1` reads as `7.0.0`.
  - Against the hard-coded `7.0.0` minimum this changes no health result: majors never carry a suffix, and every later part of the minimum is 0. It would only matter with a minimum like `7.0.4`.
- The compose e2e fixture (`ghcr.io/netxms/server-e2e:7.0.0-e2e.1`) holds 10 alarms. Their state can change once netxmsd starts polling, so a row count is not stable.
- The test helpers already build the instance through `NewDatasource` with the same settings they pass in `PluginContext`, so moving the settings onto the instance needs no test harness changes.

## Decisions

- **One NetXMS client per instance (item 3).**
  - `NewDatasource` loads the settings once. On failure it returns `failed to load plugin settings: %w`, which fails instance creation.
  - The loss is accepted: malformed `jsonData` gets a generic Grafana error instead of today's per-query message and the health check's "Unable to load settings".
  - The client holds the base URL, the API key and an `*http.Client` with a 10s timeout on the default transport. `Dispose` stays empty.
  - Ruled out: a per-request helper that still loads settings from `PluginContext` (decodes the settings on every query), and a helper for only the shared tail of each request (about half the repetition stays).
- **The shared request method** (several tasks depend on it):
  - Signature: `do(ctx, method, path string, body any) ([]byte, error)`.
  - It owns: JSON encoding and `Content-Type: application/json` only when `body != nil`, `joinURL`, the auth header, sending, reading and closing the body, and today's `== 200` success rule.
  - The alarm query passes an empty map when no root object is given, so its body stays `{}`.
  - A non-200 response returns `*upstreamError{status int, body []byte}`.
  - Local failures are wrapped with the step that failed, e.g. `failed to connect to server: %w`.
  - It does no logging. Decoding the response and building frames stay in the handlers.
- **Query errors keep today's statuses.** One `queryError(err) backend.DataResponse` replaces the copied branches:
  - upstream 401 → `StatusUnauthorized` with `Unauthorized: Invalid API key`;
  - any other upstream error → `parseErrorResponse`;
  - anything else → `StatusBadRequest` with the wrapped message.

  Redesigning the statuses for transport errors is out of scope.
- **Resource errors (item 1): JSON everywhere, 502 for anything from `do`.**
  - The two `objectId` validation errors stay 400, now with a JSON `reason`.
  - Upstream statuses still go through `writeResourceError`: always 502, with the 401 rewrite so Grafana doesn't treat it as its own session expiring, and the #27 reason/fallback behaviour.
  - Every other error from `do` is 502 with the wrapped cause in `reason`. Local failures are logged once, in the resource handler.
  - Re-marshalling the sorted object list can't fail in practice, because it encodes a map decoded from JSON. It still gets the JSON writer, with 500, since it isn't an upstream failure.
  - Ruled out: Codex's 500/502 split between errors raised inside the plugin and upstream ones. The only build step that can fail is `NewRequest` on a malformed server address, which is configuration, and the editor shows only `reason`. The user chose 502 throughout.
- **`CheckHealth`** sends its request through `do`.
  - These messages stay as they are: missing key, missing address, `Server returned status code: …`, parse failure, missing version field, version too low.
  - For an upstream status, the `(…)` part is built from the status code and `http.StatusText`. That matches today's `response.Status` for standard codes.
  - Local failures show `do`'s wrapped error. That lowercases "Failed to create request" / "Failed to connect to server". "failed to read response" now shows the error instead of the HTTP status. Both changes are accepted.
- **Colours (item 4).** One package-level table of name/colour pairs, in status order. The alarm `Severity` value mapping is built from it, and `objectStatusColor` indexes it, keeping the grey fallback. Tests keep hard-coded expected colours: if they read the table, a wrong edit to it would pass.
- **One-pass row decode (item 5).** The key-order decoder already decodes every value and throws it away. It now returns the ordered keys and the row map from that one pass, reading the bytes directly with no `json.RawMessage` step. These stay as they are: the column union in first-seen order, last value wins for a duplicate key, and rejecting malformed or non-object rows.
- **`versionAtLeast` (item 7).** Replaces `isVersionGreater`, which actually implements `>=`. This is a latent fix with no user-visible effect today (see Context), so it gets no CHANGELOG entry.
  - Each dot-separated part counts only its leading digits. `-rcN` and `-SNAPSHOT` builds of the minimum version pass, since those are the builds we test against.
  - The fourth part (service pack) compares as a number.
  - A part with no digits counts as 0, so a version string with no digits stays below the minimum.
  - No semver dependency.
- **Test layout (item 8).**
  - `datasource_fixes_test.go` is split by area: `alarm_test.go`, `dci_test.go`, `table_test.go`, `status_test.go` (object status colours), `resource_test.go`, `health_test.go`.
  - `datasource_test.go` keeps the shared helpers (`newTestDatasource`, `runSingleQuery`, `assertValueMapping`) and the query error tests that span handlers (`TestParseErrorResponse`, `TestQueryErrorsFromServerAreReported`).
  - `TestQueryData` moves to `alarm_test.go` and uses `newTestDatasource`, but keeps its own alarm payload written in the wire format and its auth-header check.
  - No configurable shared fixture. `datasource.go` stays one file.
- **e2e alarm test (item 9).** It asserts that the query succeeds and returns an `alarms` frame with the expected columns. Only checking that a `/query` request was sent would pass on a backend failure. Row counts are not asserted (see Context).
- **Out of scope:** TLS, proxy and other HTTP client options; splitting `datasource.go`; the DCI unit-as-label backlog item.

## Tandem

| Task | Writer | Reviewer |
|------|--------|----------|
| 1 | codex | claude |
| 2 | claude | codex |
| 3 | claude | codex |
| 4 | codex | claude |
| 5 | claude | codex |
| 6 | codex | claude |
| 7 | claude | codex |
| 8 | codex | claude |

## Implementation Steps

Every backend task is gated by `mage coverage` and `golangci-lint run ./...`.

### Task 1: Split the fixes test file by area

Backend tests: `pkg/plugin/datasource_fixes_test.go` (removed), `pkg/plugin/datasource_test.go`, new `alarm_test.go`, `dci_test.go`, `table_test.go`, `status_test.go`, `resource_test.go`, `health_test.go`.

This comes first, so the tests in later tasks go straight into their final files. No test changes what it asserts.

- [x] every test from the fixes file lives in the area file it belongs to; helpers are in `datasource_test.go`
- [x] `TestQueryData` builds its instance with `newTestDatasource`; its payload and auth-header check are unchanged
- [x] the same set of test functions runs before and after (compare `go test -list . ./pkg/plugin`)
- [x] commit `test: split fixes test file by area`

### Task 2: Keep one NetXMS client per datasource instance

Backend: `pkg/plugin/datasource.go` (`NewDatasource`, the client and `do`, `queryError`, the four query handlers, `CheckHealth`, the resource handler's request path).

The resource handler's request moves to `do` here. For this one commit its local failures stay plain text with status 500, with `err.Error()` as the body. Task 3 replaces that.

Tests, written first and passing against the current code before the refactor (see Context for why existing tests don't cover this):
- A query's upstream 401 gives `StatusUnauthorized` with `Unauthorized: Invalid API key`.
- A query to a closed address gives `StatusBadRequest` with the connect error.
- An alarm query with no root object sends body `{}` with `Content-Type: application/json`.
- A GET (DCI values, resource list) sends the Authorization header and no Content-Type.
- `CheckHealth` `Message` for an upstream non-200 and for a connect failure. The connect-failure case is updated in the refactor commit to the accepted lowercase wording.

After the refactor: `NewDatasource` with malformed `jsonData` returns an error that wraps the settings failure.

- [x] settings are loaded once per instance; no handler calls `LoadPluginSettings` or builds its own `http.Client`
- [x] characterization tests above added before the refactor and still pass after it, apart from the accepted `CheckHealth` wording
- [x] factory error test added
- [x] commit `refactor: keep one NetXMS client per datasource instance`

### Task 3: Return JSON reasons for resource request failures

Backend: `pkg/plugin/datasource.go` (resource handler, `handleDciList`, a JSON reason writer that takes the outgoing status), `pkg/plugin/resource_test.go`, `CHANGELOG.md`.

Tests:
- A resource call to a closed address returns 502 with a JSON `reason` containing the connect error.
- Both `handleDciList` validation errors return 400 with a JSON `reason`.
- The existing upstream error forwarding tests are unchanged.

- [x] no resource path writes a plain-text error body
- [x] local failures are logged once with the wrapped error
- [x] CHANGELOG `[Unreleased]` / Bug Fixes entry: dropdowns show the actual cause, such as a connection error, instead of "Internal Server Error" (#28)
- [x] commit `fix: return JSON reasons for resource request failures`

### Task 4: Build severity colours from one table

Backend: `pkg/plugin/datasource.go` (colour table, alarm `Severity` mapping, `objectStatusColor`), `docs/backlog/severity-colours-duplicate-status-table.md` (deleted).

- [x] the nine name/colour pairs exist once; alarm mapping and status colour are built from them
- [x] existing colour tests pass unchanged, still with their own hard-coded colours
- [x] backlog file deleted
- [x] commit `refactor: build severity colours from one table`

### Task 5: Decode table rows in one pass

Backend: `pkg/plugin/datasource.go` (row decoder, `decodeTableRows`), `pkg/plugin/table_test.go`.

Tests: `TestTableQueryColumnUnion` already covers duplicate keys and non-object rows, and must pass unchanged. Malformed row bytes can't reach the decoder through the handler, which unmarshals the outer array first. Add one direct decoder test for that case.

- [x] each row is decoded once, without the `RawMessage` step
- [x] direct malformed-row decoder test added
- [x] commit `refactor: decode table rows in one pass`

### Task 6: Compare server versions by numeric parts

Backend: `pkg/plugin/datasource.go` (`versionAtLeast`, its call in `CheckHealth`), `pkg/plugin/health_test.go`.

Tests: a `versionAtLeast` table covering:
- `7.0.0-rc2` and `7.0.0-SNAPSHOT` ≥ `7.0.0`;
- `7.0.4-rc1` ≥ `7.0.4`, and < `7.0.5`;
- `7.0.0.3` ≥ `7.0.0`;
- `7.0` = `7.0.0`;
- `6.2.1-rc1` < `7.0.0`;
- a version with no digits < `7.0.0`.

The existing `CheckHealth` version tests stay.

- [x] `isVersionGreater` is gone; health check uses `versionAtLeast`
- [x] table test added
- [x] commit `fix: compare server versions by numeric parts`

### Task 7: Remove scaffold leftovers

Backend and frontend: `pkg/plugin/datasource.go`, `pkg/main.go`, `src/types.ts`.

Remove:
- the "In this example…" comment over the interface assertions;
- the `Dispose` comment that mentions `NewSampleDatasource`;
- the `SampleDatasource` comment in `main.go`;
- the copied QueryData doc comment on `handleAlarmQuery`;
- the DisplayName comment above code that sets none;
- the unused `DataPoint` / `DataSourceResponse` types.

- [x] no `Sample` references remain in `pkg/`; `DataPoint` / `DataSourceResponse` are gone
- [x] `mage coverage`, `golangci-lint run ./...`, `yarn typecheck`, `yarn lint` pass
- [x] commit `chore: remove scaffold leftovers`

### Task 8: Assert the alarm query result in e2e

e2e: `tests/queryEditor.spec.ts`. Needs `yarn server` running.

The alarm test uses the response `refreshPanel()` returns, which already waits for `/api/ds/query`. It asserts:
- the response is OK, and `results.A` carries no `error`, since `toBeOK()` also accepts 207 multi-status;
- an `alarms` frame with the expected column names, with no row count.

- [ ] commented-out scaffold tests deleted; "chek" / "filed" test names fixed
- [ ] alarm test fails when the backend query fails (check once by hand, e.g. with a wrong `NX_API_KEY`)
- [ ] `yarn e2e` passes against the compose server; `yarn lint` passes
- [ ] commit `test: assert alarm query result in e2e` with `Closes #28`

## Post-Completion

- Manual check against the compose server:
  1. Stop the `netxms` container and open a dropdown in the query editor. The error names the connection failure, not "Internal Server Error".
  2. Start it again. The dropdowns load.
- Move this plan to `docs/plans/completed/`.
