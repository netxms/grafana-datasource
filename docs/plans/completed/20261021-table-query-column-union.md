# Table queries: build columns from all rows (#25)

## Overview

`handleTableQuery` takes the column list from the first row of the response only. Object query rows are serialized per object, so a field present only on some objects (an NXSL global assigned conditionally) is dropped from the whole frame when the first row lacks it; an empty first row yields a frame with no fields. Fix: the column list is the union of keys across all rows, in first-seen order (first row's order first, then new keys as they appear).

## Context (from discovery)

- Server behaviour, verified in netxms-master: `QueryObjects` with `readAllComputedFields` fills each row's `StringMap` from that object's own NXSL globals, so key sets differ per row; `H_GrafanaGetObjectQuery` serializes each map as is. Summary tables (`Table::toGrafanaJson`) emit every column in every row, so they are unaffected but share the handler.
- Today the body is parsed into `[]map[string]any` and then re-streamed to read the first element's key order via `decodeJSONKeyOrder`. A missing key already reads as `nil` in `buildTableColumn`, which produces null (float/bool columns) or `""` (string columns), so the column builder needs no change.
- Today `[{"A":"x"},null]` succeeds (the null row unmarshals to a nil map and is treated as empty); `[null]` already errors.
- Today a key repeated inside the first row produces duplicate frame fields (`B, A, B`): `decodeJSONKeyOrder` returns every occurrence and nothing deduplicates. Fixed as a side effect of the seen set below.

## Decisions

- **Split the body into `[]json.RawMessage`, then decode each row for both key order and values** (new helper next to `decodeJSONKeyOrder`). Ruled out: looping the existing streaming decoder over all elements (would avoid holding the payload-sized raw-row copy, but keeps the token-level code; deferred to keep the patch focused), and taking the union from the decoded maps with alphabetical order (loses the column order the query author wrote).
- Keys are deduplicated through one seen set across all rows, including a key repeated within a row: the first occurrence fixes the column position, the map decode supplies the last value. Every input row yields one frame row, including `{}`. A column exists when its key is present in any row, even if every value is null.
- **Rows must be JSON objects.** A non-object row anywhere, including `null`, fails the query with `failed to parse response: row <i>: ...` and no frame. This rejects a late null row that is accepted today; the server never emits null rows. Recorded as a behaviour change.
- The unreachable "empty array" / "expected array" branches go away, and with them the `//nolint:gocyclo` directive on `handleTableQuery`: the function drops below the `gocyclo` threshold (20) and `nolintlint` reports the directive as unused. `decodeJSONKeyOrder` stays as it is; calling it on every row is what rejects non-object rows.
- This is not a performance improvement: it parses more than before (raw split, per-row key scan, per-row map decode). Parsing stays linear in payload size; the frame itself is dense, rows × union columns, so a very sparse response grows. Acceptable for object-query volumes.
- **The plugin does not filter columns.** Known consequence: the server decides `.order` / `.visible` handling from row 0's keys only, so a hidden sort global missing from row 0 leaks into later rows and, with the union, now shows up as a column in Grafana. The response carries no visibility metadata, so the fix belongs on the server — filed as netxms/netxms#3793.

## Tandem

| Task | Writer | Reviewer |
|---|---|---|
| 1 | claude | codex |

## Implementation Steps

### Task 1: Build table query columns from the union of all rows

Backend only: `pkg/plugin/datasource.go` (`handleTableQuery`, new row-decoding helper), `pkg/plugin/datasource_fixes_test.go`, `CHANGELOG.md`.

Tests: table-driven `TestTableQueryColumnUnion` using the existing `httptest` + `newTestDatasource` + `runSingleQuery` pattern, asserting exact column names in order and, where listed, cell values and field types. Every success case also asserts one frame whose fields all have as many rows as the JSON array. `objectQueries` unless noted:
- key only in a later row: `[{"Name":"n1"},{"Name":"n2","Vendor":"Acme"}]` → `Name, Vendor`; `Vendor` = `["", "Acme"]`
- empty first row: `[{}, {"A":"x"}]` → `A`; `A` = `["", "x"]`
- first-seen order: `[{"B":"1","A":"2"},{"C":"3","A":"4"}]` → `B, A, C`
- sparse typed: `[{"Name":"n1"},{"Name":"n2","Load":1.5,"Up":false}]` → `Load` nullable float64, `Up` nullable bool; first cells nil, second cells `1.5` / `false` (guards against treating `false` as absent)
- key present but always null: `[{}, {"A":null}]` → `A`, string type, `["", ""]` (column discovery follows key presence, not values)
- duplicate key: `[{"B":"old","A":"x","B":"new"},{"C":"z"}]` → `B, A, C` once each; `B[0]` = `"new"`
- `[]`, top-level `null`, `[{}]` → frame with no fields, no error
- `[1]` → error containing `row 0`; `[{"A":"x"},null]` → error containing `row 1`
- `summaryTables` with uniform rows → column order as in the first row (covers the shared handler)

`TestObjectQuerySendsInputFieldsAndBuildsTypedColumns` stays as it is.

- [x] frame columns are the first-seen union of keys across all rows; non-object rows fail with the row index
- [x] `TestTableQueryColumnUnion` covers the cases above
- [x] `CHANGELOG.md` `[Unreleased]` / Bug Fixes: object query columns are taken from all rows, so a field set only for some objects is no longer dropped when the first object lacks it (#25). The null-row tightening goes in the commit message, not the CHANGELOG (the server never sends null rows)
- [x] `mage coverage` passes and `golangci-lint run ./...` is clean
- [x] one `fix:` commit with `Fixes #25`

## Post-Completion

- Manual check against a NetXMS server: an object query with a global assigned only for some objects (e.g. only for agent nodes, with a non-agent node first in the result) shows the column, empty for objects that don't set it.
