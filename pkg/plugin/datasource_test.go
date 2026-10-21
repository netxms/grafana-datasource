package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// newTestDatasource creates a datasource instance pointing at the given mock server.
func newTestDatasource(t *testing.T, serverURL string) (*NetXMSDatasource, backend.DataSourceInstanceSettings) {
	t.Helper()
	settings := backend.DataSourceInstanceSettings{
		JSONData:                []byte(`{"serverAddress": "` + serverURL + `"}`),
		DecryptedSecureJSONData: map[string]string{"apiKey": "test-key"},
	}
	instance, err := NewDatasource(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	ds, ok := instance.(*NetXMSDatasource)
	if !ok {
		t.Fatal("unexpected instance type")
	}
	return ds, settings
}

func runSingleQuery(t *testing.T, ds *NetXMSDatasource, settings backend.DataSourceInstanceSettings, queryType string, model map[string]any) backend.DataResponse {
	t.Helper()
	queryJSON, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{DataSourceInstanceSettings: &settings},
		Queries:       []backend.DataQuery{{RefID: "A", QueryType: queryType, JSON: queryJSON}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Responses["A"]
}

func TestParseErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantReason string
		wantStatus backend.Status
	}{
		{"reason present", http.StatusForbidden, `{"reason":"Access denied"}`, "Request error: Access denied", backend.StatusForbidden},
		{"reason with non-string fields", http.StatusInternalServerError, `{"reason":"Query error","errorCode":5,"diagnostic":{"line":1}}`, "Request error: Query error", backend.StatusInternal},
		{"empty object", http.StatusNotFound, `{}`, "Request error: 404 Not Found", backend.StatusNotFound},
		{"empty reason", http.StatusForbidden, `{"reason":""}`, "Request error: 403 Forbidden", backend.StatusForbidden},
		{"non-JSON body", http.StatusBadRequest, `Bad request`, "Request error: 400 Bad Request", backend.StatusBadRequest},
		// Query handlers handle 401 before calling parseErrorResponse; this row tests the helper itself.
		{"bare 401", http.StatusUnauthorized, "", "Request error: 401 Unauthorized", backend.StatusUnauthorized},
		{"bare 403", http.StatusForbidden, "", "Request error: 403 Forbidden", backend.StatusForbidden},
		{"bare 404", http.StatusNotFound, "", "Request error: 404 Not Found", backend.StatusNotFound},
		{"unknown status text", 599, "", "Request error: 599", backend.StatusInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := parseErrorResponse(tt.statusCode, []byte(tt.body))
			if resp.Error == nil {
				t.Fatal("expected an error response")
			}
			if got := resp.Error.Error(); got != tt.wantReason {
				t.Errorf("expected error %q, got %q", tt.wantReason, got)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("expected backend status %v, got %v", tt.wantStatus, resp.Status)
			}
		})
	}
}

func assertValueMapping(t *testing.T, field *data.Field, value, color string) {
	t.Helper()
	if field.Config == nil || len(field.Config.Mappings) != 1 {
		t.Fatalf("field %q: expected one value mapping", field.Name)
	}
	mapper, ok := field.Config.Mappings[0].(data.ValueMapper)
	if !ok {
		t.Fatalf("field %q: expected a ValueMapper, got %T", field.Name, field.Config.Mappings[0])
	}
	got, ok := mapper[value]
	if !ok || got.Text != value || got.Color != color {
		t.Errorf("field %q: expected %q mapping with text %q and color %q, got %+v (found %v)", field.Name, value, value, color, got, ok)
	}
}

func TestQueryErrorsFromServerAreReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"reason": "Query not found"}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "objectQueries", map[string]any{"objectQueryId": "99"})
	if resp.Error == nil || !strings.Contains(resp.Error.Error(), "Query not found") {
		t.Errorf("expected server reason in error, got %v", resp.Error)
	}
	resp = runSingleQuery(t, ds, settings, "summaryTables", map[string]any{"sourceObjectId": "1"})
	if resp.Error == nil || !strings.Contains(resp.Error.Error(), "summaryTableId is required") {
		t.Errorf("expected validation error, got %v", resp.Error)
	}
}

func closedServerURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	return server.URL
}

var queryTypeModels = []struct {
	queryType string
	model     map[string]any
}{
	{"alarms", map[string]any{}},
	{"dciValues", map[string]any{"sourceObjectId": "1", "dciId": "2"}},
	{"summaryTables", map[string]any{"summaryTableId": "3"}},
	{"objectQueries", map[string]any{"objectQueryId": "4"}},
	{"objectStatus", map[string]any{}},
}

func TestQueryUpstreamUnauthorizedIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"reason":"Access denied"}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	for _, tt := range queryTypeModels {
		t.Run(tt.queryType, func(t *testing.T) {
			resp := runSingleQuery(t, ds, settings, tt.queryType, tt.model)
			if resp.Status != backend.StatusUnauthorized {
				t.Errorf("expected status %v, got %v", backend.StatusUnauthorized, resp.Status)
			}
			if resp.Error == nil || resp.Error.Error() != "Unauthorized: Invalid API key" {
				t.Errorf("expected invalid API key error, got %v", resp.Error)
			}
		})
	}
}

func TestQueryConnectFailureIsReported(t *testing.T) {
	ds, settings := newTestDatasource(t, closedServerURL(t))
	for _, tt := range queryTypeModels {
		t.Run(tt.queryType, func(t *testing.T) {
			resp := runSingleQuery(t, ds, settings, tt.queryType, tt.model)
			if resp.Status != backend.StatusBadRequest {
				t.Errorf("expected status %v, got %v", backend.StatusBadRequest, resp.Status)
			}
			if resp.Error == nil || !strings.HasPrefix(resp.Error.Error(), "failed to connect to server: ") ||
				!strings.Contains(resp.Error.Error(), "connection refused") {
				t.Errorf("expected connect error, got %v", resp.Error)
			}
		})
	}
}

func TestNewDatasourceRejectsMalformedSettings(t *testing.T) {
	_, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{JSONData: []byte(`{`)})
	if _, ok := errors.AsType[*json.SyntaxError](err); !ok || !strings.HasPrefix(err.Error(), "failed to load plugin settings: ") {
		t.Errorf("expected wrapped settings error, got %v", err)
	}
}
