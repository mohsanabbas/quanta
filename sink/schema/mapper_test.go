package schema

import (
	"testing"
	"time"
)

var testSchema = Schema{
	Name: "test",
	Columns: []Column{
		{Name: "event_id", Path: "id", LogicalType: TypeString, Required: true},
		{Name: "event_type", Path: "type", LogicalType: TypeString, Required: true},
		{Name: "event_time", Path: "time", LogicalType: TypeTimestamp, Required: true},
		{Name: "provider", Path: "data.provider", LogicalType: TypeString, Required: true},
		{Name: "tokens", Path: "data.total_tokens", LogicalType: TypeInt64, Default: 0},
		{Name: "temperature", Path: "data.temperature", LogicalType: TypeFloat64, Default: 0.0},
		{Name: "stream", Path: "data.stream", LogicalType: TypeBool, Default: false},
		{Name: "missing", Path: "data.missing", LogicalType: TypeString, Default: "default_value"},
	},
}

var testJSON = []byte(`{
	"id": "evt-001",
	"type": "ai.request",
	"time": "2026-04-28T02:04:00Z",
	"data": {
		"provider": "openai",
		"total_tokens": 1500,
		"temperature": 0.7,
		"stream": true
	}
}`)

func TestMapper_Extract(t *testing.T) {
	m := NewMapper(testSchema)

	row, err := m.Extract(testJSON)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	if row["event_id"] != "evt-001" {
		t.Errorf("event_id = %v, want %q", row["event_id"], "evt-001")
	}
	if row["event_type"] != "ai.request" {
		t.Errorf("event_type = %v, want %q", row["event_type"], "ai.request")
	}
	if row["provider"] != "openai" {
		t.Errorf("provider = %v, want %q", row["provider"], "openai")
	}
	if row["tokens"] != int64(1500) {
		t.Errorf("tokens = %v (%T), want int64(1500)", row["tokens"], row["tokens"])
	}
	if row["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7", row["temperature"])
	}
	if row["stream"] != true {
		t.Errorf("stream = %v, want true", row["stream"])
	}
	if row["missing"] != "default_value" {
		t.Errorf("missing = %v, want %q", row["missing"], "default_value")
	}

	ts, ok := row["event_time"].(time.Time)
	if !ok {
		t.Fatalf("event_time is not time.Time: %T", row["event_time"])
	}
	want := time.Date(2026, 4, 28, 2, 4, 0, 0, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("event_time = %v, want %v", ts, want)
	}
}

func TestMapper_ExtractValues(t *testing.T) {
	m := NewMapper(testSchema)

	vals, err := m.ExtractValues(testJSON)
	if err != nil {
		t.Fatalf("ExtractValues() error = %v", err)
	}

	if len(vals) != len(testSchema.Columns) {
		t.Fatalf("len(vals) = %d, want %d", len(vals), len(testSchema.Columns))
	}

	if vals[0] != "evt-001" {
		t.Errorf("vals[0] = %v, want %q", vals[0], "evt-001")
	}
	if vals[3] != "openai" {
		t.Errorf("vals[3] = %v, want %q", vals[3], "openai")
	}
}

func TestMapper_ColumnNames(t *testing.T) {
	m := NewMapper(testSchema)
	names := m.ColumnNames()

	want := []string{"event_id", "event_type", "event_time", "provider", "tokens", "temperature", "stream", "missing"}
	if len(names) != len(want) {
		t.Fatalf("len(names) = %d, want %d", len(names), len(want))
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestMapper_RequiredFieldMissing(t *testing.T) {
	m := NewMapper(testSchema)

	// Missing required field "id"
	json := []byte(`{"type": "test", "time": "2026-01-01T00:00:00Z", "data": {"provider": "test"}}`)
	_, err := m.Extract(json)
	if err == nil {
		t.Fatal("Extract() expected error for missing required field")
	}
	if !contains(err.Error(), "required field") {
		t.Errorf("error = %q, want containing 'required field'", err.Error())
	}
}

func TestMapper_InvalidJSON(t *testing.T) {
	m := NewMapper(testSchema)

	_, err := m.Extract([]byte(`{invalid json`))
	if err == nil {
		t.Fatal("Extract() expected error for invalid JSON")
	}
}

func TestMapper_NestedPath(t *testing.T) {
	schema := Schema{
		Name: "nested",
		Columns: []Column{
			{Name: "deep", Path: "a.b.c.d", LogicalType: TypeString, Required: true},
		},
	}
	m := NewMapper(schema)

	json := []byte(`{"a": {"b": {"c": {"d": "found"}}}}`)
	row, err := m.Extract(json)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if row["deep"] != "found" {
		t.Errorf("deep = %v, want %q", row["deep"], "found")
	}
}

func TestMapper_ExtractFrom(t *testing.T) {
	m := NewMapper(testSchema)

	obj := map[string]any{
		"id":   "evt-002",
		"type": "ai.response",
		"time": "2026-04-28T03:00:00Z",
		"data": map[string]any{
			"provider":     "anthropic",
			"total_tokens": float64(2000),
			"temperature":  0.5,
			"stream":       false,
		},
	}

	row, err := m.ExtractFrom(obj)
	if err != nil {
		t.Fatalf("ExtractFrom() error = %v", err)
	}

	if row["event_id"] != "evt-002" {
		t.Errorf("event_id = %v, want %q", row["event_id"], "evt-002")
	}
	if row["provider"] != "anthropic" {
		t.Errorf("provider = %v, want %q", row["provider"], "anthropic")
	}
}

func TestMapper_Schema(t *testing.T) {
	m := NewMapper(testSchema)
	got := m.Schema()

	if got.Name != testSchema.Name {
		t.Errorf("Schema().Name = %q, want %q", got.Name, testSchema.Name)
	}
	if len(got.Columns) != len(testSchema.Columns) {
		t.Errorf("len(Schema().Columns) = %d, want %d", len(got.Columns), len(testSchema.Columns))
	}
}

func TestMapper_TypeCoercionError(t *testing.T) {
	schema := Schema{
		Name: "coerce_error",
		Columns: []Column{
			{Name: "num", Path: "num", LogicalType: TypeInt64, Required: true},
		},
	}
	m := NewMapper(schema)

	// String that can't be coerced to int64
	json := []byte(`{"num": "not a number"}`)
	_, err := m.Extract(json)
	if err == nil {
		t.Fatal("Extract() expected error for type coercion failure")
	}
}
