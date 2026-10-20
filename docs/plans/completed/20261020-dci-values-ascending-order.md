# DCI values: return history oldest-first (#23)

## Overview

`handleDciValues` builds the frame in the order the NetXMS history endpoint returns samples, which is newest-first. Grafana's time series panel re-sorts and hides it, but row-position reducers (Stat / Gauge / Bar gauge `Last`, `Last *`, `First`) read `values[0]` / `values[len-1]` directly and show the wrong sample. Fix: sort the samples ascending by timestamp in the backend before building the fields.

## Context (from discovery)

- Server order, verified in netxms source: `H_DataCollectionHistory` returns newest-first on every path — raw `ORDER BY idata_timestamp DESC` (stable-5.2, 6.0, 6.2, master), on-the-fly aggregation `ORDER BY 1 DESC`, tier reads `ORDER BY bucket_start DESC`.
- Raw history tables have `PRIMARY KEY(item_id, idata_timestamp)`, so equal timestamps are not expected on the raw path.
- Grafana plugin SDK `FrameMeta` has no "sorted" flag; ordering the rows is the only fix. No frame meta, frontend or e2e change.

## Decisions

- **Sort, not reverse.** A plain reverse would fix today's server but silently break the other way if the order ever changes; tier/aggregation paths are new. Sorting costs nothing noticeable at `MAX_DCI_DATA_RECORDS`.
- **Stable sort, no tie-breaker.** Stability only preserves the server's relative order among equal timestamps; it does not decide which equal-time sample is "newest". No deduplication.
- Parse first, sort, then build fields. The existing behaviour stays: null timestamps skipped, an invalid timestamp aborts the query before any frame is built, numeric/string detection unchanged.
- Out of scope: tier=auto on 6.2+ returning `avg/min/max` instead of `value` — filed as #29.

## Tandem

| Task | Writer | Reviewer |
|---|---|---|
| 1 | claude | codex |

## Implementation Steps

### Task 1: Sort DCI history samples ascending

Backend only: `pkg/plugin/datasource.go` (`handleDciValues`), `pkg/plugin/datasource_fixes_test.go`, `CHANGELOG.md`.

Tests use the existing `httptest` + `newTestDatasource` + `runSingleQuery("dciValues")` pattern and assert exact (time, value) pairs, comparing times with `time.Equal`:
- newest-first fixture with a null-timestamp point in the middle → ascending pairs, null point dropped
- shuffled fixture → ascending pairs (this is what proves sort over reverse; existing ascending fixtures guard against an unconditional reverse)
- `TestDciValuesStringFallback` fixture reversed to newest-first, both string values asserted in ascending order
- malformed timestamp → error response, no frames
- empty `values` → numeric frame with empty, equal-length time and value fields

`TestDciValuesSkipNullTimestampAndKeepNumericType` and the `datasource_test.go` fixtures stay as they are.

- [x] samples are sorted ascending by timestamp before the fields are built
- [x] new and updated tests above are in place
- [x] `CHANGELOG.md` has an `[Unreleased]` / Bug Fixes entry: DCI history is returned oldest-first, so Stat / Gauge / Bar gauge `Last` and `First` show the correct sample (#23)
- [x] `mage coverage` passes and `golangci-lint run ./...` is clean
- [x] one `fix:` commit with `Fixes #23`

## Post-Completion

- Manual check against a NetXMS server: Stat panel with `Last` on a DCI shows the most recent value, `First` shows the oldest in range.
