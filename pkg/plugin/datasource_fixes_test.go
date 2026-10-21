package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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

func TestObjectQuerySendsInputFieldsAndBuildsTypedColumns(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/grafana/infinity/object-query" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"Name": "node1", "Load": 1.5, "Up": true, "Note": "x"},
			{"Name": "node2", "Load": null, "Up": false, "Note": 42}
		]`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "objectQueries", map[string]any{
		"objectQueryId":   "7",
		"sourceObjectId":  "3",
		"queryParameters": `{"name": "srv", "limit": 5}`,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	inputFields, ok := received["inputFields"].(map[string]any)
	if !ok {
		t.Fatalf("inputFields object missing from request body: %v", received)
	}
	if inputFields["name"] != "srv" || inputFields["limit"] != float64(5) {
		t.Errorf("unexpected inputFields: %v", inputFields)
	}
	if _, exists := received["values"]; exists {
		t.Errorf("legacy 'values' key must not be sent")
	}
	if received["queryId"] != float64(7) || received["rootObjectId"] != float64(3) {
		t.Errorf("unexpected ids in request body: %v", received)
	}

	if len(resp.Frames) != 1 {
		t.Fatalf("expected one frame, got %d", len(resp.Frames))
	}
	frame := resp.Frames[0]
	want := map[string]data.FieldType{
		"Name": data.FieldTypeString,
		"Load": data.FieldTypeNullableFloat64,
		"Up":   data.FieldTypeNullableBool,
		"Note": data.FieldTypeString, // mixed column falls back to text
	}
	if len(frame.Fields) != len(want) {
		t.Fatalf("expected %d fields, got %d", len(want), len(frame.Fields))
	}
	for _, f := range frame.Fields {
		if f.Type() != want[f.Name] {
			t.Errorf("column %q: expected type %s, got %s", f.Name, want[f.Name], f.Type())
		}
		if f.Len() != 2 {
			t.Errorf("column %q: expected 2 rows, got %d", f.Name, f.Len())
		}
	}
	if v, ok := frame.Fields[1].ConcreteAt(1); ok {
		t.Errorf("null cell must stay null, got %v", v)
	}
	if v, _ := frame.Fields[3].ConcreteAt(1); v != "42" {
		t.Errorf("mixed column must be rendered as text, got %v", v)
	}
}

func TestTableQueryColumnUnion(t *testing.T) {
	type column struct {
		name   string
		typ    data.FieldType
		values []any // nil element is a null cell
	}
	str := func(name string, values ...any) column {
		return column{name: name, typ: data.FieldTypeString, values: values}
	}
	tests := []struct {
		name      string
		queryType string
		body      string
		rows      int
		columns   []column
		wantErr   string
	}{
		{
			name: "key only in a later row",
			body: `[{"Name":"n1"},{"Name":"n2","Vendor":"Acme"}]`,
			rows: 2,
			columns: []column{
				str("Name", "n1", "n2"),
				str("Vendor", "", "Acme"),
			},
		},
		{
			name:    "empty first row",
			body:    `[{}, {"A":"x"}]`,
			rows:    2,
			columns: []column{str("A", "", "x")},
		},
		{
			name: "first-seen order",
			body: `[{"B":"1","A":"2"},{"C":"3","A":"4"}]`,
			rows: 2,
			columns: []column{
				str("B", "1", ""),
				str("A", "2", "4"),
				str("C", "", "3"),
			},
		},
		{
			name: "sparse typed columns",
			body: `[{"Name":"n1"},{"Name":"n2","Load":1.5,"Up":false}]`,
			rows: 2,
			columns: []column{
				str("Name", "n1", "n2"),
				{name: "Load", typ: data.FieldTypeNullableFloat64, values: []any{nil, 1.5}},
				{name: "Up", typ: data.FieldTypeNullableBool, values: []any{nil, false}},
			},
		},
		{
			name:    "key present but always null",
			body:    `[{}, {"A":null}]`,
			rows:    2,
			columns: []column{str("A", "", "")},
		},
		{
			name: "duplicate key",
			body: `[{"B":"old","A":"x","B":"new"},{"C":"z"}]`,
			rows: 2,
			columns: []column{
				str("B", "new", ""),
				str("A", "x", ""),
				str("C", "", "z"),
			},
		},
		{name: "empty array", body: `[]`},
		{name: "null response", body: `null`},
		{name: "single empty row", body: `[{}]`, rows: 1},
		{name: "non-object row", body: `[1]`, wantErr: "row 0"},
		{name: "late null row", body: `[{"A":"x"},null]`, wantErr: "row 1"},
		{
			name:      "summary table",
			queryType: "summaryTables",
			body:      `[{"Node":"n1","CPU":"5 %"},{"Node":"n2","CPU":"7 %"}]`,
			rows:      2,
			columns: []column{
				str("Node", "n1", "n2"),
				str("CPU", "5 %", "7 %"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			queryType, model := "objectQueries", map[string]any{"objectQueryId": "1"}
			if tt.queryType == "summaryTables" {
				queryType, model = tt.queryType, map[string]any{"summaryTableId": "1"}
			}
			ds, settings := newTestDatasource(t, server.URL)
			resp := runSingleQuery(t, ds, settings, queryType, model)

			if tt.wantErr != "" {
				if resp.Error == nil || !strings.Contains(resp.Error.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, resp.Error)
				}
				if len(resp.Frames) != 0 {
					t.Errorf("expected no frames on error, got %d", len(resp.Frames))
				}
				return
			}
			if resp.Error != nil {
				t.Fatalf("unexpected error: %v", resp.Error)
			}
			if len(resp.Frames) != 1 {
				t.Fatalf("expected one frame, got %d", len(resp.Frames))
			}
			fields := resp.Frames[0].Fields
			if len(fields) != len(tt.columns) {
				t.Fatalf("expected %d fields, got %d", len(tt.columns), len(fields))
			}
			for i, want := range tt.columns {
				f := fields[i]
				if f.Name != want.name {
					t.Errorf("field %d: expected name %q, got %q", i, want.name, f.Name)
				}
				if f.Type() != want.typ {
					t.Errorf("column %q: expected type %s, got %s", f.Name, want.typ, f.Type())
				}
				if f.Len() != tt.rows {
					t.Errorf("column %q: expected %d rows, got %d", f.Name, tt.rows, f.Len())
					continue
				}
				for row, wantValue := range want.values {
					got, ok := f.ConcreteAt(row)
					if wantValue == nil {
						if ok {
							t.Errorf("column %q row %d: expected null, got %v", f.Name, row, got)
						}
						continue
					}
					if got != wantValue {
						t.Errorf("column %q row %d: expected %v, got %v", f.Name, row, wantValue, got)
					}
				}
			}
		})
	}
}

func TestParseInputFields(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    map[string]any
		wantErr bool
	}{
		{name: "object", raw: `{"a": "1", "b": 2}`, want: map[string]any{"a": "1", "b": float64(2)}},
		{name: "array of objects", raw: `[{"a": "1"}, {"b": "2"}]`, want: map[string]any{"a": "1", "b": "2"}},
		{name: "array of key/value pairs", raw: `[{"key": "a", "value": "1"}]`, want: map[string]any{"a": "1"}},
		{name: "invalid json", raw: `{`, wantErr: true},
		{name: "scalar", raw: `"x"`, wantErr: true},
		{name: "array of scalars", raw: `[1]`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInputFields(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q: expected %v, got %v", k, v, got[k])
				}
			}
		})
	}
}

func TestResourceHandlerForwardsUpstreamErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"reason": "Invalid API key"}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)

	var gotStatus int
	var gotBody string
	sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
		gotStatus = res.Status
		gotBody += string(res.Body)
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
		t.Errorf("expected upstream 401 to be reported as 502, got %d", gotStatus)
	}
	if !strings.Contains(gotBody, "Invalid API key") {
		t.Errorf("expected reason in body, got %q", gotBody)
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

func runDciQuery(t *testing.T, ds *NetXMSDatasource, settings backend.DataSourceInstanceSettings, timeRange backend.TimeRange, maxDataPoints int64) backend.DataResponse {
	t.Helper()
	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{DataSourceInstanceSettings: &settings},
		Queries: []backend.DataQuery{{
			RefID:         "A",
			QueryType:     "dciValues",
			JSON:          []byte(`{"sourceObjectId": "1", "dciId": "2"}`),
			MaxDataPoints: maxDataPoints,
			TimeRange:     timeRange,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Responses["A"]
}

func TestDciValuesSkipNullTimestampAndKeepNumericType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/objects/10/data-collection/20/history") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"description": "CPU", "unitName": "%", "values": [
			{"timestamp": "2026-10-19T10:00:00.000Z", "value": "1.5"},
			{"timestamp": null, "value": "0"},
			{"timestamp": "2026-10-19T10:01:00.000Z", "value": "2"}
		]}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "dciValues", map[string]any{"sourceObjectId": "10", "dciId": "20"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if len(resp.Frames) != 1 || len(resp.Frames[0].Fields) != 2 {
		t.Fatalf("unexpected frame layout: %v", resp.Frames)
	}
	valueField := resp.Frames[0].Fields[1]
	if valueField.Type() != data.FieldTypeFloat64 {
		t.Errorf("expected float64 values, got %s", valueField.Type())
	}
	if valueField.Len() != 2 {
		t.Errorf("expected the null-timestamp point to be skipped, got %d points", valueField.Len())
	}
	if resp.Frames[0].Name != "CPU" {
		t.Errorf("expected frame name from description, got %q", resp.Frames[0].Name)
	}
}

func TestDciValuesStringFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"description": "State", "unitName": "", "values": [
			{"timestamp": "2026-10-19T10:01:00.000Z", "value": "up"},
			{"timestamp": "2026-10-19T10:00:00.000Z", "value": "1"}
		]}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "dciValues", map[string]any{"sourceObjectId": "1", "dciId": "2"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	valueField := resp.Frames[0].Fields[1]
	if valueField.Type() != data.FieldTypeString || valueField.Len() != 2 {
		t.Fatalf("expected 2 string values, got %s x%d", valueField.Type(), valueField.Len())
	}
	if valueField.At(0) != "1" || valueField.At(1) != "up" {
		t.Errorf("expected values oldest-first [1 up], got [%v %v]", valueField.At(0), valueField.At(1))
	}
}

func TestDciValuesSortedAscending(t *testing.T) {
	tests := []struct {
		name   string
		values string
	}{
		{"newest first", `[
			{"timestamp": "2026-10-19T10:02:00.000Z", "value": "30"},
			{"timestamp": "2026-10-19T10:01:00.000Z", "value": "20"},
			{"timestamp": null, "value": "0"},
			{"timestamp": "2026-10-19T10:00:00.000Z", "value": "10"}
		]`},
		{"shuffled", `[
			{"timestamp": "2026-10-19T10:01:00.000Z", "value": "20"},
			{"timestamp": "2026-10-19T10:02:00.000Z", "value": "30"},
			{"timestamp": "2026-10-19T10:00:00.000Z", "value": "10"}
		]`},
	}
	base := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	wantTimes := []time.Time{base, base.Add(time.Minute), base.Add(2 * time.Minute)}
	wantValues := []float64{10, 20, 30}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"description": "CPU", "unitName": "%", "values": ` + tt.values + `}`))
			}))
			defer server.Close()

			ds, settings := newTestDatasource(t, server.URL)
			resp := runSingleQuery(t, ds, settings, "dciValues", map[string]any{"sourceObjectId": "1", "dciId": "2"})
			if resp.Error != nil {
				t.Fatalf("unexpected error: %v", resp.Error)
			}
			timeField, valueField := resp.Frames[0].Fields[0], resp.Frames[0].Fields[1]
			if timeField.Len() != len(wantTimes) || valueField.Len() != len(wantValues) {
				t.Fatalf("expected %d points, got %d times / %d values", len(wantTimes), timeField.Len(), valueField.Len())
			}
			for i := range wantTimes {
				gotTime, _ := timeField.At(i).(time.Time)
				if !gotTime.Equal(wantTimes[i]) || valueField.At(i) != wantValues[i] {
					t.Errorf("row %d: expected %s=%v, got %v=%v", i, wantTimes[i], wantValues[i], timeField.At(i), valueField.At(i))
				}
			}
		})
	}
}

func TestDciValuesInvalidTimestampFailsQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"description": "CPU", "unitName": "%", "values": [
			{"timestamp": "2026-10-19T10:00:00.000Z", "value": "1"},
			{"timestamp": "yesterday", "value": "2"}
		]}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "dciValues", map[string]any{"sourceObjectId": "1", "dciId": "2"})
	if resp.Error == nil || !strings.Contains(resp.Error.Error(), "yesterday") {
		t.Errorf("expected timestamp parse error, got %v", resp.Error)
	}
	if len(resp.Frames) != 0 {
		t.Errorf("expected no frames on error, got %d", len(resp.Frames))
	}
}

func TestDciValuesEmptySeries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"description": "CPU", "unitName": "%", "values": []}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "dciValues", map[string]any{"sourceObjectId": "1", "dciId": "2"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if len(resp.Frames) != 1 || len(resp.Frames[0].Fields) != 2 {
		t.Fatalf("unexpected frame layout: %v", resp.Frames)
	}
	timeField, valueField := resp.Frames[0].Fields[0], resp.Frames[0].Fields[1]
	if valueField.Type() != data.FieldTypeFloat64 || timeField.Len() != 0 || valueField.Len() != 0 {
		t.Errorf("expected empty float64 series, got %s with %d times / %d values", valueField.Type(), timeField.Len(), valueField.Len())
	}
}

func TestDciValuesForwardsMaxDataPoints(t *testing.T) {
	tests := []struct {
		name          string
		maxDataPoints int64
		want          string
	}{
		{"positive budget is forwarded", 640, "640"},
		{"zero is omitted", 0, ""},
		{"negative is omitted", -1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var query url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query = r.URL.Query()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"description": "CPU", "values": []}`))
			}))
			defer server.Close()

			ds, settings := newTestDatasource(t, server.URL)
			from := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
			resp := runDciQuery(t, ds, settings, backend.TimeRange{From: from, To: from.Add(time.Hour)}, tt.maxDataPoints)
			if resp.Error != nil {
				t.Fatalf("unexpected error: %v", resp.Error)
			}
			if got := query.Get("maxDataPoints"); got != tt.want {
				t.Errorf("expected maxDataPoints=%q, got %q", tt.want, got)
			}
			if query.Get("timeFrom") != "1792404000" || query.Get("timeTo") != "1792407600" {
				t.Errorf("expected time range in Unix seconds, got timeFrom=%s timeTo=%s", query.Get("timeFrom"), query.Get("timeTo"))
			}
		})
	}
}

func TestDciValuesAggregated(t *testing.T) {
	tests := []struct {
		name         string
		response     string
		wantInterval float64
	}{
		// buckets narrower than the polling interval, the first one starting before the query From
		{"raw buckets", `{"description": "CPU", "tierServed": "raw", "aggregated": true, "bucketSize": 1000, "values": [
			{"timestamp": "2026-10-19T10:01:00.000Z", "avg": 0, "min": 0, "max": 0},
			{"timestamp": "2026-10-19T10:02:00.000Z", "avg": 30.5, "min": 30, "max": 31},
			{"timestamp": "2026-10-19T09:59:59.000Z", "avg": 10, "min": 10, "max": 10}
		]}`, 0},
		{"hourly tier", `{"description": "CPU", "tierServed": "hourly", "aggregated": true, "values": [
			{"timestamp": "2026-10-19T11:00:00.000Z", "avg": 30.5, "sampleCount": 60},
			{"timestamp": "2026-10-19T10:00:00.000Z", "avg": 0, "sampleCount": 60},
			{"timestamp": "2026-10-19T09:00:00.000Z", "avg": 10, "sampleCount": 60}
		]}`, 3600000},
		{"daily tier", `{"description": "CPU", "tierServed": "daily", "aggregated": true, "values": [
			{"timestamp": "2026-10-21T00:00:00.000Z", "avg": 30.5, "sampleCount": 1440},
			{"timestamp": "2026-10-20T00:00:00.000Z", "avg": 0, "sampleCount": 1440},
			{"timestamp": null, "avg": 99, "sampleCount": 1},
			{"timestamp": "2026-10-19T00:00:00.000Z", "avg": 10, "sampleCount": 1440}
		]}`, 86400000},
		{"row without avg is skipped", `{"description": "CPU", "tierServed": "raw", "aggregated": true, "bucketSize": 1000, "values": [
			{"timestamp": "2026-10-19T10:00:03.000Z", "avg": 30.5},
			{"timestamp": "2026-10-19T10:00:02.000Z", "value": "77"},
			{"timestamp": "2026-10-19T10:00:01.000Z", "avg": 0},
			{"timestamp": "2026-10-19T10:00:00.000Z", "avg": 10}
		]}`, 0},
	}
	wantValues := []float64{10, 0, 30.5}
	from := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	timeRange := backend.TimeRange{From: from, To: from.Add(3 * time.Minute)}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			ds, settings := newTestDatasource(t, server.URL)
			resp := runDciQuery(t, ds, settings, timeRange, 100)
			if resp.Error != nil {
				t.Fatalf("unexpected error: %v", resp.Error)
			}
			timeField, valueField := resp.Frames[0].Fields[0], resp.Frames[0].Fields[1]
			if valueField.Type() != data.FieldTypeFloat64 || valueField.Len() != len(wantValues) {
				t.Fatalf("expected %d float64 values, got %s x%d", len(wantValues), valueField.Type(), valueField.Len())
			}
			for i, want := range wantValues {
				if valueField.At(i) != want {
					t.Errorf("row %d: expected %v, got %v", i, want, valueField.At(i))
				}
				if i > 0 && !timeField.At(i).(time.Time).After(timeField.At(i-1).(time.Time)) {
					t.Errorf("row %d: timestamps not ascending", i)
				}
			}
			var gotInterval float64
			if timeField.Config != nil {
				gotInterval = timeField.Config.Interval
			}
			if gotInterval != tt.wantInterval {
				t.Errorf("expected time field interval %v, got %v", tt.wantInterval, gotInterval)
			}
			if notices := frameNotices(resp.Frames[0]); len(notices) != 0 {
				t.Errorf("unexpected notices: %v", notices)
			}
		})
	}
}

func TestDciValuesRawSeriesHasNoInterval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"description": "CPU", "tierServed": "raw", "values": [
			{"timestamp": "2026-10-19T10:00:00.000Z", "value": "1"}
		]}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "dciValues", map[string]any{"sourceObjectId": "1", "dciId": "2"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if cfg := resp.Frames[0].Fields[0].Config; cfg != nil {
		t.Errorf("expected no time field config for raw series, got %+v", cfg)
	}
}

func TestDciValuesRowLimitNotice(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		row        func(ts string) string
		rows       int
		wantNotice bool
	}{
		{"raw rows at limit", `"tierServed": "raw"`, rawRow, maxDciDataRecords, true},
		{"raw rows below limit", `"tierServed": "raw"`, rawRow, maxDciDataRecords - 1, false},
		{"string rows at limit", `"tierServed": "raw"`, func(ts string) string { return `{"timestamp": "` + ts + `", "value": "up"}` }, maxDciDataRecords, true},
		{"hourly tier at limit", `"tierServed": "hourly", "aggregated": true`, avgRow, maxDciDataRecords, true},
		{"raw buckets at limit", `"tierServed": "raw", "aggregated": true, "bucketSize": 1000`, avgRow, maxDciDataRecords, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body strings.Builder
			body.WriteString(`{"description": "CPU", ` + tt.header + `, "values": [`)
			base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			for i := range tt.rows {
				if i > 0 {
					body.WriteByte(',')
				}
				body.WriteString(tt.row(base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)))
			}
			body.WriteString(`]}`)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body.String()))
			}))
			defer server.Close()

			ds, settings := newTestDatasource(t, server.URL)
			resp := runSingleQuery(t, ds, settings, "dciValues", map[string]any{"sourceObjectId": "1", "dciId": "2"})
			if resp.Error != nil {
				t.Fatalf("unexpected error: %v", resp.Error)
			}
			notices := frameNotices(resp.Frames[0])
			if tt.wantNotice && (len(notices) != 1 || notices[0].Severity != data.NoticeSeverityWarning) {
				t.Errorf("expected one warning notice, got %v", notices)
			}
			if !tt.wantNotice && len(notices) != 0 {
				t.Errorf("expected no notices, got %v", notices)
			}
		})
	}
}

func frameNotices(frame *data.Frame) []data.Notice {
	if frame.Meta == nil {
		return nil
	}
	return frame.Meta.Notices
}

func rawRow(ts string) string { return `{"timestamp": "` + ts + `", "value": "1"}` }

func avgRow(ts string) string { return `{"timestamp": "` + ts + `", "avg": 1}` }

func TestCheckHealthRequiresServerVersion(t *testing.T) {
	tests := []struct {
		version    string
		wantStatus backend.HealthStatus
	}{
		{"6.2.1", backend.HealthStatusError},
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
