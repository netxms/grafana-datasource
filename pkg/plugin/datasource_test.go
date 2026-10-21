package plugin

import (
	"context"
	"encoding/json"
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
