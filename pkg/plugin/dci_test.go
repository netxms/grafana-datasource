package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

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

func TestDciValuesRequestIsAuthorizedGetWithoutContentType(t *testing.T) {
	var gotMethod, gotAuth string
	gotContentType := []string{"unset"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Values("Content-Type")
		_, _ = w.Write([]byte(`{"values": []}`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runDciQuery(t, ds, settings, backend.TimeRange{}, 0)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if gotMethod != http.MethodGet || gotAuth != "Bearer test-key" || len(gotContentType) != 0 {
		t.Errorf("expected authorized GET without Content-Type, got %s, %q, %q", gotMethod, gotAuth, gotContentType)
	}
}
