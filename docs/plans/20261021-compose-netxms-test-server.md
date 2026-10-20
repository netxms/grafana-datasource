# NetXMS test server in docker compose

## Overview

Issue #21, target 2.0.2. `docker compose up` must start a working NetXMS next to Grafana with no env vars or secrets, for plugin catalog reviewers ("Provisioning provided: Yes") and local dev. CI uses the same compose setup instead of its own `services:` container.

The work spans two repos. The `netxms/docker` part (Tasks 1-3) comes first: every `ghcr.io/netxms/server-e2e` image after 6.0.3 is broken, and no fixture key can be published.

Design agreed with Codex (peer review) and approved by the maintainer.

## Context (from discovery)

- **Broken e2e images.** `docker/server-e2e/files/db.sqlite3` is at schema 60.34 and was last updated 2026-03-02 (`a39a352`). Every later release needs a newer schema (6.0.4 = 60.36 … 6.2.6 = 62.44, 7.0.0 = 70.53). `server-e2e:7.0.0` exits after ~4s with "Your database has format version 60.34, but server is compiled for version 70.53". `build-images.yml` never runs the image, so the build still passes. This is why CI is pinned to 6.0.3.
- **Token format** (same in 6.0.3 and 7.0.0, `auth-token.h`, `authtokens.cpp`):
  - the server generates 20 random bytes;
  - the bearer key is their lowercase base32 form (32 chars);
  - `auth_tokens.token_data` is the uppercase hex SHA-256 of the raw 20 bytes, not of the key text.

  The fixture has two tokens for `admin` (user_id 1), expiring 2145969332 (2038). Their plaintext is lost, including the one behind the `E2E_TEST_API_KEY` secret.
- **`nxdbmgr upgrade` exits 0 on failure.** `UpgradeDatabase()` returns `void` and never sets `exitCode`. Filed as netxms/netxms#3794. A second run prints "Core database schema is up to date" only when major/minor match, then "Database upgrade succeeded/failed".
- **Docker repo tags** come only from `release-X.Y.Z` git tags, through `docker/metadata-action`. A run from master produces only `latest`. Moving `release-7.0.0` would rebuild all four images (`agent`, `server`, `server-e2e`, `web`) and rerun create-release, so it is ruled out. `server-e2e` is amd64 only.
- **What the e2e tests use from the fixture.** Today only the authenticated server-info health check and the dropdown loading (objects). The alarm query assertion is a commented-out TODO. The fixture's data should still survive the upgrade.
- **Compose's default Grafana is unsupported.** `.config/docker-compose-base.yaml` defaults `GRAFANA_VERSION` to 12.2.0, below the plugin's `grafanaDependency` `>=12.4.0`.
- **`server-e2e` has no HTTP client.** Its base is `debian:13-slim` plus `ca-certificates` and `netxms-server`, with no curl or wget.
- **`build-images.yml` already has an input-less `workflow_dispatch`** running the full matrix. On master the `latest` rule (`enable={{is_default_branch}}`) is true for it.
- **The local Grafana container is arm64 on Apple Silicon**, so `mage build:linux` (amd64 only) isn't enough locally; use `mage buildAll`.
- **`@grafana/plugin-e2e` interpolation.** `readProvisionedDataSource` fills `$VAR` / `${VAR}` / `${VAR:-x}` in the provisioning YAML from the Playwright host `process.env`. Defaults set only in compose never reach the tests.
- **Grafana provisioning** expands `$VAR` / `${VAR}`. It does not support `${VAR:-default}`.
- **Do not edit `.config/`** (scaffolding). Top-level `grafana.environment` in `docker-compose.yaml` merges into the inherited service.
- **Docker repo convention:** its CLAUDE.md says to write no comments.

## Solution Overview

- **Fixture build.** The `server-e2e` image upgrades its copy of the fixture to the installed server's schema at build time. Future releases then produce working e2e images on their own.
- **Committed fixture stays at 60.34.** `nxdbmgr` can't downgrade, so a 70.53 source fixture would stop older versions from building.
- **One public fixture key.** The fixture gets one new token meant to be public. A credential for a throwaway sqlite DB guards nothing. Publishing it removes the CI secret and lets Dependabot PRs run e2e.
- **Smoke test before push.** The exact `server-e2e` image goes through an authenticated smoke test before it is pushed.
- **7.0.0 republish.** A narrow manual publish produces `server-e2e:7.0.0-e2e.1` once. The existing 7.0.0 images and the `release-7.0.0` tag stay untouched.
- **This repo pins that tag.** A committed `.env` is the single source of fixture defaults. Compose reads it for interpolation, and Playwright loads it with Node's built-in `process.loadEnvFile`. Neither overwrites variables already set in the shell.
- **Ruled out:**
  - keeping `NX_API_KEY` as a required secret and giving reviewers the key out of band (not zero-config, Dependabot stays blocked);
  - a compose profile for NetXMS (plain `docker compose up` must work);
  - publishing host ports for NetXMS (only Grafana's backend talks to it, and host ports would collide with a developer's own server);
  - creating tokens at container start.

## Tandem

| Task | Writer | Reviewer |
|---|---|---|
| 1 | codex | claude |
| 2 | codex | claude |
| 3 | codex | claude |
| 4 | maintainer (manual) | — |
| 5 | claude | codex |
| 6 | claude | codex |

Tasks 1-3 are committed in `/Users/alk/Development/netxms/docker` on branch `compose-netxms-test-server`, one commit per task with the `Tandem-Task: N` trailer. Their checkboxes and drift notes live in this plan, so the writer also makes a companion commit here (same trailer) and amends both until the reviewer approves. The run stops at Task 4 until the maintainer confirms the published image.

## Implementation Steps

### Task 1: Public fixture token (netxms/docker)

**Layers:** server-e2e fixture DB, docs.

- **Key generation:** 20 random bytes give the key (lowercase base32) and its hash (uppercase hex SHA-256 of the bytes).
- **Token table:** replace both `auth_tokens` rows with one row for user_id 1, description making clear it is the public e2e fixture token, expiry 2145969332.
- **Safe edit:** work on a scratch copy, checkpoint the WAL, close all connections, then copy back only `db.sqlite3`. No `-shm` / `-wal` files may be left in the checkout.

- [x] fixture has exactly one token; schema still 60.34; `PRAGMA integrity_check` ok
- [x] plaintext key documented in a new server-e2e section of the docker repo README, stated as public and for tests only; server-e2e added to the docker repo CLAUDE.md component list
- [x] verified: base32-decode the documented key, SHA-256 it, and get a hash equal to `token_data`

### Task 2: Upgrade fixture at image build time (netxms/docker)

**Layers:** `server-e2e/Dockerfile`.

- **Ordering:** the upgrade RUN goes after both `COPY files/db.sqlite3` and `COPY files/netxmsd.conf`. Those come after the `VOLUME` lines, so anything a RUN writes under `/etc/netxms` or `/var/lib/netxms` is discarded; `/tmp/db.sqlite3` is unaffected.
- **Install curl** in the image. Without an HTTP client, nothing can probe the WebAPI from inside the container. The compose healthcheck (Task 5) and the CI smoke test (Task 3) both need one. This must be settled before the Task 4 publish, otherwise it costs a `7.0.0-e2e.2`.
- **Background upgrades:** if `PendingOnlineUpgrades` is set after the upgrade, run `nxdbmgr background-upgrade` at build time too. Otherwise netxmsd raises `EVENT_PENDING_DATABASE_UPGRADE` on every start and changes the fixture's event/alarm state.

- **Upgrade step:** after the package installs, run `nxdbmgr -c /etc/netxmsd.conf upgrade` on `/tmp/db.sqlite3`. The explicit config makes sure the fixture is upgraded and not some default DB.
- **Schema check:** the build must fail if the schema doesn't match the installed server afterwards, without relying on the `nxdbmgr` exit code (#3794). No ignore-errors flags.
- **Usable check:** a second `nxdbmgr upgrade` run must report the core schema up to date and the upgrade succeeded.

- [x] local amd64 build at 7.0.0 starts and serves authenticated `GET /v1/server-info` with the Task 1 key
- [x] build fails when the upgrade can't reach the target schema (check by hand, e.g. a corrupted or too-new fixture)
- [x] the fixture's objects, DCIs and alarms are still present after the upgrade, and `PendingOnlineUpgrades` is empty

Validation: the built fixture is at 70.53 with integrity intact, all 87 objects, 757 DCIs, and 10 alarms retained, and no pending background upgrades. Authenticated startup took 3.6 seconds under amd64 emulation on Apple Silicon. A scratch fixture at schema 99.34 was rejected by the build's schema check despite `nxdbmgr upgrade` returning zero.

### Task 3: Smoke test and narrow publish in CI (netxms/docker)

**Layers:** `.github/workflows/build-images.yml`.

- **Same image tested and shipped:** for `server-e2e`, build and load amd64 without pushing, smoke-test that image, then push that same image. A second build after the test could produce a different artifact. Pushing a loaded image with `docker push` drops the provenance attestation `build-push-action` adds today; that's accepted for server-e2e.
- **Smoke test:**
  - start the container;
  - poll `GET /v1/server-info` with `Authorization: Bearer <public key>` against an overall deadline, using `docker exec` and the image's curl;
  - fail at once if the container exits;
  - print container logs on failure;
  - always remove the container and volumes.
- **PRs:** run the smoke test with no registry secrets.
- **Other components:** `agent`, `server` and `web` keep their current build-and-push behaviour.
- **Manual publish:** `workflow_dispatch` takes `component` and an explicit `tag`. It replaces today's input-less dispatch. Rules:
  - only the selected component is built;
  - only the given tag is pushed, never `latest` and never the `X.Y` alias;
  - the tag must match `^<NETXMS_VERSION>-.+`. The suffix is required, so `7.0.0` (the release tag) can't be overwritten, and a 7.0.1 build can't be labelled `7.0.0-…`;
  - the same smoke test runs before pushing;
  - the `tag` input reaches shell steps through `env:`, not inline `${{ }}`.

- [ ] PR run builds and smoke-tests `server-e2e` without pushing
- [ ] dispatch with a mismatched or suffix-less tag (`7.0.1-e2e.1`, `7.0.0`) is rejected before building
- [ ] a dispatch run pushes only `<component>:<tag>`: no `latest`, no other components
- [ ] release-tag behaviour for all four components is unchanged apart from the smoke gate

### Task 4: Publish `server-e2e:7.0.0-e2e.1` (manual checkpoint)

The maintainer does this: push the docker repo, then trigger the dispatch. Tasks 5-6 depend on the published image.

- [ ] docker repo changes merged to master
- [ ] dispatch run with `component=server-e2e`, `tag=7.0.0-e2e.1` succeeded, smoke test included
- [ ] `ghcr.io/netxms/server-e2e:7.0.0-e2e.1` pulls, starts, and answers the public key

Tasks 5 and 6 land in one PR, and CI is judged on its head. Task 5 alone leaves CI red: Grafana with `network_mode: host` can't resolve `netxms`, and the step-level `NX_API_KEY` secret overrides `.env`.

### Task 5: Compose NetXMS service and shared fixture defaults

**Layers:** `docker-compose.yaml`, `provisioning/`, repo-root `.env`, `playwright.config.ts`, tests, README, CLAUDE.md.

- **`netxms` service:**
  - image `ghcr.io/netxms/server-e2e:7.0.0-e2e.1`, `platform: linux/amd64`;
  - healthcheck: authenticated `curl` to `/v1/server-info` from inside the container (curl added in Task 2). `nxadm -c help` only proves the control socket is up. Tune the timing under emulation on Apple Silicon;
  - no host ports.
- **`grafana` service:** `depends_on: netxms: condition: service_healthy`. Map `NX_SERVER_ADDRESS` and `NX_API_KEY` explicitly in `grafana.environment`, because `.env` only drives interpolation and doesn't reach the container.
- **Provisioning:** `serverAddress: $NX_SERVER_ADDRESS`, `apiKey: $NX_API_KEY`. No `:-` defaults in the YAML.
- **`.env`:**
  - literal values only (Node's parser doesn't substitute);
  - `NX_SERVER_ADDRESS=http://netxms:8000/` and `NX_API_KEY=<public key>`;
  - `GRAFANA_VERSION=12.4.0`, the plugin's floor, bumped together with `grafanaDependency`. This overrides the 12.2.0 compose default without touching `.config/`, and CI's shell variables still win;
  - one comment line marking them as public fixture defaults.
- **`playwright.config.ts`:** `process.loadEnvFile` with a path resolved next to the config file, not the working directory.
- **`tests/configEditor.spec.ts`:** drop the stale host.docker.internal comment and the `process.env.NX_API_KEY` precedence, which `readProvisionedDataSource` already covers.
- **README, local development:**
  - the build steps before `docker compose up` (`yarn build`, `mage buildAll`), since `dist/` is mounted from the checkout;
  - zero-config `docker compose up`;
  - pointing at your own server: export `NX_SERVER_ADDRESS` / `NX_API_KEY` in the shell so both compose and Playwright see them (e.g. `NX_SERVER_ADDRESS=http://host.docker.internal:8000/`, not `localhost`), then `docker compose up --no-deps grafana`;
  - a stale exported `NX_API_KEY`, even an empty one, overrides `.env` for both compose and Playwright;
  - NetXMS runs under amd64 emulation on Apple Silicon.
- **CLAUDE.md:** update the Local Development section to match.

- [ ] clean checkout, `NX_*` and `GRAFANA_*` unset: `yarn build && mage buildAll`, then `docker compose up`, and Grafana 12.4.0 starts with the provisioned datasource health check passing
- [ ] `yarn e2e` passes against it on the host
- [ ] own-server override works with exported vars
- [ ] `yarn typecheck`, `yarn lint`, `yarn test:ci` pass (typecheck doesn't cover `playwright.config.ts`; lint and e2e do)
- [ ] local plugin validator run with `-sourceCodeUri file://./` doesn't flag the committed key in `.env`
- [ ] CHANGELOG `[Unreleased]` entry: zero-config compose with a bundled NetXMS test server

### Task 6: CI uses compose for NetXMS

**Layers:** `.github/workflows/ci.yml`, `docker-compose.ci.yaml`, CLAUDE.md.

- **Remove:**
  - the `services: netxms-server` block;
  - `docker-compose.ci.yaml` (host networking existed only to reach the service container) and every `-f docker-compose.ci.yaml`;
  - every use of `secrets.E2E_TEST_API_KEY`;
  - the Dependabot e2e skip. Keep the `has-e2e` condition.
- **`publish-report` on Dependabot:** once e2e runs there, `publish-report` follows. `GITHUB_TOKEN` is read-only on Dependabot `pull_request` runs, so the gh-pages deploy will probably fail. Gate it for Dependabot.
- **Start:** both services with the matrix Grafana image and version vars as now.
- **Keep:** wait-for-grafana and the Grafana version check.
- **Cleanup:** runs under `always()` with the same compose invocation.
- **Logs:** on `failure()` (not only a failed test step), print `docker compose logs` for both services to the job output. With `service_healthy`, an unhealthy netxms fails at `compose up` and the test step never runs.
- **CLAUDE.md:** update the CI section's e2e image line.

- [ ] PR run: e2e passes on every matrix Grafana version
- [ ] a forced failure at startup (unhealthy netxms) and at test time each show logs from both containers, and cleanup still runs
- [ ] a Dependabot PR runs e2e green end to end, `publish-report` included

## Post-Completion

- Delete the `E2E_TEST_API_KEY` repository secret once Task 6 is merged.
- Close #21 with the 2.0.2 release.
- Submitting to the plugin catalog no longer needs a key handed over separately: reviewers run `docker compose up`.
- NetXMS 7.0.1 and later: no action needed. The release tag build upgrades and smoke-tests the fixture. Bump the image here when moving e2e to a new server version.
