# Project profile: NetXMS Grafana datasource

## What it is
- Grafana datasource plugin `radensolutions-netxms-datasource`, published in the Grafana plugin catalog and signed
- Go backend (`pkg/plugin/datasource.go`, one large file) proxies Grafana queries and resource requests to the NetXMS webAPI over HTTP with a Bearer API key
- TypeScript/React frontend (`src/`): query editor, config editor, resource fetches via `getResource`
- Supported: NetXMS server 5.2.4+ with the webapi module, Grafana 12.4+ (built against Grafana 13 packages)

## What a real failure looks like
- A panel shows wrong data silently: misdecoded fields, wrong timestamps, dropped or duplicated series, wrong severity/state mapping
- One bad row or null value from NetXMS fails a whole query or crashes the backend process
- A query that should work returns an error, or an error is swallowed and the panel goes empty with no message
- The API key leaks: logged, returned to the browser, put in jsonData instead of secureJsonData, or sent somewhere other than the configured server
- Error status codes that make Grafana misbehave (e.g. a 401 from a resource call reads as an expired Grafana session)
- Query editor state bugs: query not re-run, fields reset, stale dropdowns after a datasource or object change
- Anything the Grafana plugin validator would reject (it blocks releases)

## Blast radius
- Runs inside customers' Grafana; a backend panic takes the plugin down for every dashboard using it
- NetXMS webAPI is plain HTTP and trusted admin-configured; the server address is not attacker-controlled (SSRF-style taint on it is not a finding)
- Data volume can be large (DCI history over long ranges, many objects for status maps)

## Reporting bar
- Report correctness, robustness against real NetXMS responses, and security of the API key
- Do not report style nits the linters already enforce (`.golangci.yml`, `eslint.config.mjs`, prettier)
- Do not report missing features or "could also support X" unless the existing code claims to support it
- Test gaps are worth reporting only where a real bug class above is untested

## Deliberate conventions
- `.config/` is `@grafana/create-plugin` scaffolding; never review it
- Comments only where something is unexpected; absence of comments is not a finding
- Single large `datasource.go` is accepted; splitting it is not a finding on its own
- 10-second HTTP client timeout is intentional
- `docs/completes/` plans are historical and never edited, even if they drift from the code; drift there is not a finding
