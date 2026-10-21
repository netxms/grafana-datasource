package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		actual   string
		required string
		want     bool
	}{
		{"7.0.0-rc2", "7.0.0", true},
		{"7.0.0-SNAPSHOT", "7.0.0", true},
		{"7.0.4-rc1", "7.0.4", true},
		{"7.0.4-rc1", "7.0.5", false},
		{"7.0.0.3", "7.0.0", true},
		{"7.0.0.10", "7.0.0.3", true},
		{"7.0.0.3", "7.0.0.10", false},
		{"7.0", "7.0.0", true},
		{"7.0.0", "7.0", true},
		{"6.2.1-rc1", "7.0.0", false},
		{"SNAPSHOT", "7.0.0", false},
		{"7.x.4", "7.0.4", true},
		{"8.0.0", "7.0.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.actual+" >= "+tt.required, func(t *testing.T) {
			if got := versionAtLeast(tt.actual, tt.required); got != tt.want {
				t.Errorf("versionAtLeast(%q, %q) = %v, want %v", tt.actual, tt.required, got, tt.want)
			}
		})
	}
}

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

func TestCheckHealthReportsRequestFailures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	tests := []struct {
		name       string
		serverURL  string
		wantPrefix string
	}{
		{"upstream status", upstream.URL, "Server returned status code: 503 (503 Service Unavailable)"},
		{"connect failure", closedServerURL(t), "failed to connect to server: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, settings := newTestDatasource(t, tt.serverURL)
			res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{
				PluginContext: backend.PluginContext{DataSourceInstanceSettings: &settings},
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != backend.HealthStatusError || !strings.HasPrefix(res.Message, tt.wantPrefix) {
				t.Errorf("expected error starting with %q, got %v %q", tt.wantPrefix, res.Status, res.Message)
			}
		})
	}
}
