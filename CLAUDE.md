# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

NetXMS datasource plugin for Grafana. Hybrid Go backend + TypeScript/React frontend plugin that queries the NetXMS monitoring system via its web API. Plugin ID: `radensolutions-netxms-datasource`.

Requires NetXMS server 5.2.4+ with webAPI module enabled. Requires Grafana 10.4.0+.

## Common Commands

### Build
```bash
yarn build              # Frontend production build (outputs to dist/)
yarn dev                # Frontend watch mode with live reload
mage buildAll           # Backend build (Go binaries to dist/)
mage build:linux        # Backend build for Linux only
```

### Test
```bash
yarn test               # Jest unit tests (watch mode)
yarn test:ci            # Jest unit tests (CI, no watch)
mage coverage           # Go backend tests with coverage
yarn e2e                # Playwright e2e tests (needs running Grafana)
```

### Lint & Typecheck
```bash
yarn lint               # ESLint
yarn lint:fix           # ESLint + Prettier auto-fix
yarn typecheck          # TypeScript type checking
```

### Local Development
```bash
yarn server             # Starts Grafana in Docker with plugin mounted
```
Grafana runs at http://localhost:3000. The provisioned datasource expects NetXMS webAPI at `http://host.docker.internal:8000/` with API key from `$NX_API_KEY` env var.

## Architecture

### Frontend → Backend Communication

1. **Resource requests**: Frontend `datasource.ts` calls `getResource("/path")` → backend HTTP handlers serve dropdown data (object lists, DCI lists, etc.)
2. **Query requests**: Grafana calls `QueryData` → backend `QueryTypeMux` routes to handler by query type → handler calls NetXMS API → returns Grafana data frames

### Query Types
- `alarms` — alarm list with severity/state color coding
- `dciValues` — time-series DCI history data
- `summaryTables` — tabular data with dynamic columns
- `objectQueries` — custom queries with optional JSON parameters
- `objectStatus` — object status with color-coded mappings (one frame per object)

### Backend (Go, `pkg/`)
- `pkg/main.go` — entry point, registers plugin with Grafana SDK
- `pkg/models/settings.go` — config deserialization (serverAddress + apiKey)
- `pkg/plugin/datasource.go` — all query handlers, resource endpoints, health check. This is the main file (~1000 lines)

All NetXMS API calls use Bearer token auth. HTTP client has 10-second timeout.

### Frontend (TypeScript/React, `src/`)
- `src/module.ts` — plugin registration
- `src/datasource.ts` — extends `DataSourceWithBackend`, resource fetch methods, query validation
- `src/types.ts` — `NetXMSQuery`, `NetxmsSourceOptions`, `NetXMSSecureJsonData` interfaces
- `src/components/QueryEditor.tsx` — query builder UI with conditional fields per query type
- `src/components/ConfigEditor.tsx` — server address + API key inputs

### Build Tooling
- Frontend: Webpack 5 (config in `.config/webpack/`), SWC for transpilation
- Backend: Go 1.26+, Mage with Grafana Plugin SDK build targets (`Magefile.go`). Binary name: `gpx_netxms` (configured in `src/plugin.json`)
- Testing: Jest (`.config/jest/`), Playwright (`playwright.config.ts`)
- Scaffolding config in `.config/` is from `@grafana/create-plugin` — avoid modifying these files directly

### CI/CD (GitHub Actions)
- `ci.yml` — typecheck, lint, test, build, sign, validate, e2e on multiple Grafana versions
- `release.yml` — triggered by `v*` tags, builds and publishes to Grafana plugin registry
- E2e tests use `ghcr.io/netxms/server-e2e:6.0.3` as the NetXMS test server
