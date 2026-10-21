package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestCheckHealthRequiresServerVersion(t *testing.T) {
	tests := []struct {
		version    string
		wantStatus backend.HealthStatus
	}{
		{"6.2.1", backend.HealthStatusError},
		{"7.0", backend.HealthStatusOk},
		{"7.0.0", backend.HealthStatusOk},
		{"7.1.0", backend.HealthStatusOk},
		{"8.0", backend.HealthStatusOk},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"version": "` + tt.version + `"}`))
			}))
			defer server.Close()

			ds, settings := newTestDatasource(t, server.URL)
			res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{
				PluginContext: backend.PluginContext{DataSourceInstanceSettings: &settings},
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != tt.wantStatus {
				t.Errorf("expected status %v, got %v (%s)", tt.wantStatus, res.Status, res.Message)
			}
		})
	}
}
