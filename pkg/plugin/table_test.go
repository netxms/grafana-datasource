package plugin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

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
