package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObjectStatusColor(t *testing.T) {
	tests := []struct {
		name   string
		status int32
		color  string
	}{
		{"Normal", 0, "rgb(0, 137, 0)"},
		{"Warning", 1, "rgb(0, 142, 145)"},
		{"Minor", 2, "rgb(201, 198, 0)"},
		{"Major", 3, "rgb(223, 102, 0)"},
		{"Critical", 4, "rgb(160, 0, 0)"},
		{"Unknown", 5, "rgb(33, 33, 248)"},
		{"Unmanaged", 6, "rgb(113, 113, 113)"},
		{"Disabled", 7, "rgb(100, 41, 0)"},
		{"Testing", 8, "rgb(138, 0, 143)"},
		{"negative", -1, "rgb(128, 128, 128)"},
		{"past last status", 9, "rgb(128, 128, 128)"},
		{"large", 2147483647, "rgb(128, 128, 128)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := objectStatusColor(tt.status); got != tt.color {
				t.Errorf("status %d: expected %q, got %q", tt.status, tt.color, got)
			}
		})
	}
}

func TestObjectStatusNullAndOutOfRangeColors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"Name":"null status","Status":null},{"Name":"invalid status","Status":9}]`))
	}))
	defer server.Close()

	ds, settings := newTestDatasource(t, server.URL)
	resp := runSingleQuery(t, ds, settings, "objectStatus", map[string]any{"sourceObjectId": "1"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if len(resp.Frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(resp.Frames))
	}
	for i, want := range []struct{ name, color string }{
		{"null status", "rgb(0, 137, 0)"}, // JSON null decodes to status 0 (Normal).
		{"invalid status", "rgb(128, 128, 128)"},
	} {
		frame := resp.Frames[i]
		if frame.Name != want.name {
			t.Errorf("expected frame %q, got %q", want.name, frame.Name)
		}
		if len(frame.Fields) != 1 {
			t.Fatalf("frame %q: expected one field, got %d", frame.Name, len(frame.Fields))
		}
		field := frame.Fields[0]
		if field.Name != "Name" || field.Len() != 1 {
			t.Fatalf("frame %q: expected one Name value, got field %q with %d rows", frame.Name, field.Name, field.Len())
		}
		if got, ok := field.ConcreteAt(0); !ok || got != want.name {
			t.Errorf("expected Name %q, got %v", want.name, got)
		}
		assertValueMapping(t, field, want.name, want.color)
	}
}
