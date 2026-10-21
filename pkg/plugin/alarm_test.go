package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// mockAlarmResponse creates a test server that returns mock alarm data
func mockAlarmResponse() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		// Use wire-format keys independently of alarmResponse's JSON tags.
		_, _ = w.Write([]byte(`[
			{"Id":1,"Severity":"Critical","State":"Outstanding","Source":"First Source",
			 "Message":"First Alarm","Count":2,"Ack/Resolve by":"",
			 "Created":"2026-10-19T10:00:00.000Z","Last Change":"2026-10-19T10:05:00.000Z"},
			{"Id":3,"Severity":"Major","State":"Acknowledged","Source":"Second Source",
			 "Message":"Second Alarm","Count":4,"Ack/Resolve by":"Test User",
			 "Created":"2026-10-20T11:00:00.000Z","Last Change":"2026-10-20T11:10:00.000Z"}
		]`))
	}))
}

func TestQueryData(t *testing.T) {
	// Create mock server
	mockServer := mockAlarmResponse()
	defer mockServer.Close()

	ds, settings := newTestDatasource(t, mockServer.URL)

	// Create a query model
	qm := queryModel{
		SourceObjectId: "123",
	}
	queryJSON, err := json.Marshal(qm)
	if err != nil {
		t.Fatal(err)
	}

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
		t.Fatal(err)
	}

	if len(resp.Responses) != 1 {
		t.Fatal("QueryData must return a response")
	}

	// Verify the response contains our mock data
	response := resp.Responses["A"]
	if response.Error != nil {
		t.Fatalf("Expected no error, got: %v", response.Error)
	}

	if len(response.Frames) != 1 {
		t.Fatal("Expected one data frame")
	}

	frame := response.Frames[0]
	if frame.Name != "alarms" {
		t.Errorf("Expected frame name 'alarms', got: %s", frame.Name)
	}

	if len(frame.Fields) != 9 {
		t.Fatalf("Expected 9 fields, got: %d", len(frame.Fields))
	}

	wantFields := []struct {
		name   string
		values [2]any
	}{
		{"Id", [2]any{uint32(1), uint32(3)}},
		{"Severity", [2]any{"Critical", "Major"}},
		{"State", [2]any{"Outstanding", "Acknowledged"}},
		{"Source", [2]any{"First Source", "Second Source"}},
		{"Message", [2]any{"First Alarm", "Second Alarm"}},
		{"Count", [2]any{uint32(2), uint32(4)}},
		{"Ack/Resolve by", [2]any{"", "Test User"}},
		{"Created", [2]any{
			time.Date(2026, time.October, 19, 10, 0, 0, 0, time.UTC),
			time.Date(2026, time.October, 20, 11, 0, 0, 0, time.UTC),
		}},
		{"Last Change", [2]any{
			time.Date(2026, time.October, 19, 10, 5, 0, 0, time.UTC),
			time.Date(2026, time.October, 20, 11, 10, 0, 0, time.UTC),
		}},
	}
	for i, want := range wantFields {
		field := frame.Fields[i]
		if field.Name != want.name {
			t.Errorf("field %d: expected name %q, got %q", i, want.name, field.Name)
		}
		if field.Len() != len(want.values) {
			t.Fatalf("field %q: expected %d rows, got %d", field.Name, len(want.values), field.Len())
		}
		for row, value := range want.values {
			if got, ok := field.ConcreteAt(row); !ok || got != value {
				t.Errorf("field %q row %d: expected %v (%T), got %v (%T)", field.Name, row, value, value, got, got)
			}
		}
	}
	assertValueMapping(t, frame.Fields[1], "Critical", "rgb(160, 0, 0)")
	assertValueMapping(t, frame.Fields[2], "Acknowledged", "greenyellow")
}

func TestAlarmsAcceptUnsignedIds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"Id": 4000000000, "Severity": "Major", "State": "Outstanding", "Source": "n", "Message": "m",
			"Count": 3000000000, "Ack/Resolve by": "", "Created": "2026-10-19T10:00:00.000Z", "Last Change": "2026-10-19T10:00:00.000Z"}]`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "alarms", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if got, _ := resp.Frames[0].Fields[0].ConcreteAt(0); got != uint32(4000000000) {
		t.Errorf("expected alarm id 4000000000, got %v", got)
	}
}

func TestAlarmQueryWithoutRootObjectSendsEmptyObject(t *testing.T) {
	var gotBody, gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "alarms", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if gotBody != "{}" {
		t.Errorf("expected body {}, got %q", gotBody)
	}
	if gotContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", gotContentType)
	}
}
