# Require Grafana 12.2.0 and make e2e test the resolved versions (#26)

## Overview

`src/plugin.json` declares `grafanaDependency: ">=10.4.0"`, but the query type selector in `QueryEditor.tsx` uses `Combobox`, which `@grafana/ui` exports only from 11.5.0. On 10.4–11.4 the query editor cannot render. CI didn't catch this because the e2e matrix never passed the Grafana version to the container, so every job ran the compose default. Fix the CI matrix so each job runs its own version, add a check that fails when it doesn't, and raise the floor to 12.2.0.

## Context (from discovery)

- `Combobox` export checked with `npm pack @grafana/ui@<v>`: absent from `dist/index.d.ts` in 10.4.19, 11.2.10 and 11.4.0, present in 11.5.0. Codex confirmed the same in Grafana's `packages/grafana-ui/src/components/index.ts` at v11.4.0 and v11.5.0. It has been in the query editor since the initial commit. `82297a9` switched the other dropdowns to `Select` but missed this one.
- `ci.yml` `playwright-tests` sets `matrix: include: ${{ fromJson(...) }}`, which flattens each `{name, version}` entry to `matrix.name` / `matrix.version`. "Start Grafana" reads `matrix.GRAFANA_IMAGE.VERSION` / `.NAME`, which evaluate to empty, so compose falls back to `grafana-enterprise:12.2.0` from `.config/docker-compose-base.yaml`. In run 39487858915 every job built that image, including the job named `grafana-enterprise@10.4.19`. The flattened shape dates from `bea9ba8`. The job name and the report `artifact-prefix` use the flattened form, and "Start Grafana" uses the nested form.
- The upstream create-plugin template uses `matrix: GRAFANA_IMAGE: ${{fromJson(...)}}` with `matrix.GRAFANA_IMAGE.*` throughout. GitHub expression property lookup is case-insensitive, so `NAME`/`VERSION` match the lowercase keys the resolver outputs.
- `grafana/plugin-actions/e2e-version` (default `plugin-grafana-dependency` mode) returns the latest patch of each minor in the `grafanaDependency` range, up to 6 entries including nightly. `evenlyPickVersions` always keeps the first and last entry, so the latest 12.2.x will always be in the matrix.
- The CHANGELOG entry for 2.0.1 says "Grafana 10.4+ is still supported and covered by e2e tests". That claim is wrong.

## Decisions

- Floor is `>=12.2.0`, the version CI actually verified. `Combobox` stays. Ruled out: switching to `Select` to keep 10.4 (more support surface than we can verify), and `>=11.5`/`>=12.0` (versions never tested).
- Matrix goes back to the scaffold shape rather than changing "Start Grafana" to the flattened keys, so future `cp-update` diffs stay small.
- The version check fails closed. Only the literal `nightly` skips the equality check, and nothing else is skipped. The first draft skipped the check whenever the expected version wasn't numeric, which would have passed on an empty value, the exact failure this plan fixes.
- An exact 12.2.0 row isn't worth adding. The latest 12.2.x is always tested, and we assume patch releases don't remove exports. That is an assumption, not a guarantee.
- Out of scope: `docker compose pull` runs before the image/version variables are set. This is harmless on a fresh runner.
- The release version (a breaking change, so likely 3.0.0) is decided at release time, not here.

## Tandem

| Task | Writer | Reviewer |
|---|---|---|
| 1 | claude | codex |
| 2 | codex | claude |

## Implementation Steps

### Task 1: Run e2e against the resolved Grafana version

CI only (`.github/workflows/ci.yml`, job `playwright-tests`).

New step "Verify Grafana version", placed after "Wait for grafana server":
- `shell: bash`. Pass expected name and version to the script through `env:`, not by interpolating them into the script.
- Fail if the expected name or version is empty.
- Actual version: `curl -fsS localhost:3000/api/health | jq -er '.version | select(type == "string" and . != "")'`. The job uses `network_mode: host`, and `/api/health` needs no auth. A curl or jq failure must fail the step, so don't run the pipeline inside `local x=$(…)`, `echo "…$(…)"`, `if` or `||`, where `set -e` doesn't see it. Log the actual version on every row.
- If the expected version is `nightly`, log the version and pass. This does not prove the nightly image ran; for that row the "Start Grafana" `FROM` line is the only evidence.
- Otherwise the expected version must match `^[0-9]+\.[0-9]+\.[0-9]+$`, or the step fails. Pass only when the actual version equals the expected one or equals the expected one followed by `+…` (use a bash `case`). Reject `-…` (prerelease) and longer numbers such as `12.2.01`.

- [ ] matrix is `GRAFANA_IMAGE: ${{ fromJson(needs.resolve-versions.outputs.matrix) }}`; the job name, "Start Grafana" and the report `artifact-prefix` all use `matrix.GRAFANA_IMAGE.NAME` / `.VERSION`; a case-insensitive grep finds no flattened `matrix.name` / `matrix.version` references (the current file has `matrix.VERSION`)
- [ ] "Verify Grafana version" step implemented as above
- [ ] step body run locally with a stubbed `curl`/env, with the expected result for each case: empty VERSION → fail, empty NAME → fail, VERSION `garbage` → fail, `12.2.4` vs `12.2.4` → pass, `12.2.4` vs `12.2.4+security-01` → pass, `12.2.0` vs `12.2.01` → fail, `12.2.0` vs `12.2.0-rc1` → fail, `12.2.0` vs `12.2.0garbage` → fail, health JSON with `"version": null`, `"version": ""` or no `version` → fail, health body that isn't JSON → fail, stubbed `curl` exiting non-zero → fail, `nightly` with any non-empty version → pass, `nightly` with no version → fail
- [ ] `actionlint` (if available) is clean on `ci.yml`
- [ ] one `fix(ci): run e2e against the resolved Grafana version` commit

### Task 2: Require Grafana 12.2.0

Plugin metadata and docs: `src/plugin.json`, `src/README.md`, `CLAUDE.md`, `.revmux/profile.md`, `.github/dependabot.yml`, `CHANGELOG.md`. Don't touch completed plans in `docs/plans/completed/`.

- [ ] `grafanaDependency` is `>=12.2.0`
- [ ] `src/README.md` requirements say "Grafana 12.2.0 or later"; `CLAUDE.md` says "Requires Grafana 12.2.0+"; `.revmux/profile.md` says Grafana 12.2+; the comment on the React 19 ignore in `dependabot.yml` says the Grafana 12.x plugin runtime (the ignore itself stays)
- [ ] `CHANGELOG.md` `[Unreleased]` has a new `### Breaking Changes` section, placed first (above Bug Fixes and Changes), with an entry: requires Grafana 12.2.0 or later (was 10.4.0), because the query type selector uses `Combobox`, which isn't available before Grafana 11.5, so on 10.4–11.4 the query editor did not render (#26)
- [ ] `CHANGELOG.md` `[Unreleased]` / Bug Fixes entry: the e2e matrix didn't pass the Grafana version to the container, so jobs ran the compose default instead of the resolved versions; now fixed and verified by a version check. The released 2.0.1 entry stays unchanged
- [ ] `yarn typecheck`, `yarn lint`, `yarn test:ci`, `yarn build` pass
- [ ] one `fix: require Grafana 12.2.0` commit with `Closes #26`

## Post-Completion

These steps need CI on a PR branch, never main:
- Push the Task 1 commit on its own first. The matrix is still resolved from `>=10.4.0` at that point. Expected result: every row passes "Verify Grafana version"; the 10.4.x and 11.2.x rows fail in the query editor specs, which shows #26 happening at runtime and not only in the `.d.ts` check; 11.6.x and later pass. If the old rows don't fail, re-check the CHANGELOG claim before writing it.
- After Task 2: the resolved matrix contains the latest 12.2.x. Each stable job's "Start Grafana" log shows `FROM grafana/grafana-enterprise:<matrix version>`, "Verify Grafana version" passes, and the nightly job's `FROM` line shows `nightly`.
- Negative test: push a temporary commit that makes only the "Start Grafana" version expression render empty (not another valid tag), with the check still reading the correct version. Confirm every stable row fails at "Verify Grafana version", then drop the commit before merge. This works because the compose default (12.2.0) differs from every matrix row today; it would stop working if the default were bumped to match a row.
- A green matrix covers query type selection and the Alarms root object field only; the data assertions in `tests/queryEditor.spec.ts` are commented out. Don't describe it as validating query types or data. If 12.x/13.x/nightly fails, diagnose the cause; don't loosen locators or the check.
