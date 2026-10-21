package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestResourceHandlerForwardsUpstreamErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantReason string
	}{
		{"reason overrides 401", http.StatusUnauthorized, `{"reason":"Invalid API key"}`, "Invalid API key"},
		{"reason overrides 403", http.StatusForbidden, `{"reason":"Access denied"}`, "Access denied"},
		{"reason with non-string fields", http.StatusForbidden, `{"reason":"Query error","errorCode":5}`, "Query error"},
		{"bare 401", http.StatusUnauthorized, "", "Unauthorized: Invalid API key"},
		{"bare 403", http.StatusForbidden, "", "Request error: 403 Forbidden"},
		{"bare 404", http.StatusNotFound, "", "Request error: 404 Not Found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			ds, settings := newTestDatasource(t, server.URL)
			var gotStatus int
			var gotBody []byte
			sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
				gotStatus = res.Status
				gotBody = append(gotBody, res.Body...)
				return nil
			})
			err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
				PluginContext: backend.PluginContext{DataSourceInstanceSettings: &settings},
				Path:          "alarmObjects",
				Method:        http.MethodGet,
			}, sender)
			if err != nil {
				t.Fatal(err)
			}
			if gotStatus != http.StatusBadGateway {
				t.Errorf("expected upstream %d to be reported as 502, got %d", tt.statusCode, gotStatus)
			}
			var parsed struct {
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(gotBody, &parsed); err != nil {
				t.Fatalf("invalid JSON %q: %v", gotBody, err)
			}
			if parsed.Reason != tt.wantReason {
				t.Errorf("expected reason %q, got %q", tt.wantReason, parsed.Reason)
			}
		})
	}
}

func TestResourceHandlerSortsObjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"objects": [{"name": "b", "id": 2}, {"name": "a", "id": 1}]}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	var gotBody string
	sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
		if res.Status != http.StatusOK {
			t.Errorf("expected 200, got %d", res.Status)
		}
		gotBody += string(res.Body)
		return nil
	})
	if err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		PluginContext: backend.PluginContext{DataSourceInstanceSettings: &settings},
		Path:          "dcis",
		URL:           "dcis?objectId=5",
		Method:        http.MethodGet,
	}, sender); err != nil {
		t.Fatal(err)
	}
	var parsed ObjectList
	if err := json.Unmarshal([]byte(gotBody), &parsed); err != nil {
		t.Fatalf("invalid JSON %q: %v", gotBody, err)
	}
	if len(parsed.Objects) != 2 || parsed.Objects[0].Name != "a" || parsed.Objects[1].Name != "b" {
		t.Errorf("objects not sorted by name: %v", parsed.Objects)
	}
}

// ObjectList mirrors the resource response consumed by the frontend.
type ObjectList struct {
	Objects []struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	} `json:"objects"`
}
