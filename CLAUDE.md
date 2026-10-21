# CLAUDE.md

NetXMS datasource plugin for Grafana: Go backend (`pkg/`) + TypeScript/React frontend (`src/`), querying NetXMS through its WebAPI (server needs the webAPI module).

## Commands
- `yarn test` is watch mode and never exits; use `yarn test:ci`. Backend tests: `mage coverage`.
- Build the backend with `mage buildAll` locally (not `build:linux`): the Grafana container is arm64 on Apple Silicon.
- `yarn server` (`docker compose up --build`) starts Grafana at http://localhost:3000 plus a `netxms` test server (`ghcr.io/netxms/server-e2e`, amd64 only). `yarn e2e` needs it running.

## Dev environment
- The provisioned datasource reads `$NX_SERVER_ADDRESS` / `$NX_API_KEY`. Defaults are in the committed `.env`, read by Compose and loaded by `playwright.config.ts`. The key is public and only valid for the test server. Shell variables override `.env`.
- Keep `GRAFANA_VERSION` out of `.env`: `@grafana/plugin-e2e` prefers it over the running Grafana's version. Its default lives in `docker-compose.yaml`.
- `.config/` is `@grafana/create-plugin` scaffolding, refreshed by `cp-update.yml`. Don't edit it.

## CI and release
- e2e needs no secrets, so Dependabot PRs run it too. `publish-report` is skipped for them (read-only token).
- The plugin validator is blocking. Unfixable vulnerabilities are excepted, with an expiry, in `.github/osv-scanner-validator.toml`.
- `v*` tags create a draft GitHub release. Publishing it and submitting the zip URL to the Grafana catalog (grafana.com → My Plugins → Submit Plugin) is manual.
