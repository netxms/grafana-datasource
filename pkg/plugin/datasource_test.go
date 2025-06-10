package plugin

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// mockAlarmResponse creates a test server that returns mock alarm data
func mockAlarmResponse() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Debug print headers
		log.Printf("Mock server received headers: %v", r.Header)

		// Verify request headers
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Mock alarm response
		alarms := []alarmResponse{
			{
				Id:         1,
				Severity:   "Critical",
				State:      "Active",
				Source:     "Test Source",
				Message:    "Test Alarm",
				Count:      1,
				AckBy:      "Test User",
				Created:    time.Now(),
				LastChange: time.Now(),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(alarms)
	}))
}

func TestQueryData(t *testing.T) {
	// Create mock server
	mockServer := mockAlarmResponse()
	defer mockServer.Close()

	// Create plugin settings with mock server URL
	settings := backend.DataSourceInstanceSettings{
		JSONData: []byte(`{
			"serverAddress": "` + mockServer.URL + `"
		}`),
		DecryptedSecureJSONData: map[string]string{
			"apiKey": "test-key",
		},
	}

	instance, err := NewDatasource(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}

	ds, ok := instance.(*Datasource)
	if !ok {
		t.Fatal("Failed to type assert instance to *Datasource")
	}

	// Create a query model
	queryModel := queryModel{
		SourceObjectId: "123",
	}
	queryJSON, err := json.Marshal(queryModel)
	if err != nil {
		t.Fatal(err)
	}
	// Debug print
	t.Logf("Query JSON: %s", string(queryJSON))

	resp, err := ds.QueryData(
		context.Background(),
		&backend.QueryDataRequest{
			PluginContext: backend.PluginContext{
				DataSourceInstanceSettings: &settings,
			},
			Queries: []backend.DataQuery{
				{
					RefID:     "A",
					QueryType: "alarms",
					JSON:      queryJSON,
				},
			},
		},
	)
	if err != nil {
		t.Error(err)
	}

	if len(resp.Responses) != 1 {
		t.Fatal("QueryData must return a response")
	}

	// Verify the response contains our mock data
	response := resp.Responses["A"]
	if response.Error != nil {
		t.Errorf("Expected no error, got: %v", response.Error)
	}

	if len(response.Frames) != 1 {
		t.Fatal("Expected one data frame")
	}

	frame := response.Frames[0]
	if frame.Name != "alarms" {
		t.Errorf("Expected frame name 'alarms', got: %s", frame.Name)
	}

	// Verify the frame contains our mock data
	if len(frame.Fields) != 9 {
		t.Errorf("Expected 9 fields, got: %d", len(frame.Fields))
	}
}
