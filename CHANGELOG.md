# Changelog

All notable changes to this project will be documented in this file.

## [2.1.1] - 2026-10-26

### Changes
- Connection errors show a short message such as "Unable to connect to the NetXMS server" or "Request timed out" instead of the full error with the server address; the full error is in the Grafana server log
- Requests follow Grafana's response size limit setting, and the default request timeout is 30 seconds instead of 10
- The Query parameters field of object queries uses the standard Grafana text area and fills the editor width

### Security
- Updated the Grafana plugin SDK and other dependencies

## [2.1.0] - 2026-10-21

### Breaking Changes
- Requires NetXMS 7.0.0 or later (was 5.2.4)
- Requires Grafana 12.4.0 or later (was 10.4.0)

### Bug Fixes
- Long DCI history ranges are no longer cut off at the start. NetXMS now averages history down to about the panel's resolution, and a warning appears if a series still hits the server's 200000-row limit (#24)
- DCI history that NetXMS serves from hourly or daily aggregates is shown instead of an empty series (#24)
- Stat, Gauge and Bar gauge panels show the correct first and last values; they were swapped (#23)
- Object query columns no longer go missing when the first object lacks that field (#25)
- Error messages show the actual cause, such as a connection error or HTTP status, instead of "Internal Server Error" (#27, #28)
- The object query no longer reruns on every keystroke in the Query parameters field (#27)

### Security
- Backend built with Go 1.26.10 and an updated `golang.org/x/net`, fixing vulnerabilities in the Go standard library and HTTP/2 handling

## [2.0.1] - 2026-10-20

### Bug Fixes
- Object query parameters now take effect; NetXMS ignored them before
- The query runs as soon as the last required field in the editor is selected
- Dropdowns show NetXMS errors, such as an invalid API key, instead of staying empty
- Summary tables and object queries with numeric, boolean or empty cells no longer crash the plugin
- A DCI history point without a timestamp no longer fails the whole series
- A NetXMS error in a dropdown no longer looks like an expired Grafana session
- The config editor's server address example no longer includes `/api`

### Security
- Updated Go, the Grafana plugin SDK, grpc and npm dependencies to fix reported vulnerabilities, including CVE-2026-33186

## [2.0.0] - 2026-01-15

- Initial release: alarms, DCI values, summary tables, object status and object queries from NetXMS, with API key authentication
