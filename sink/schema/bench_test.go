package schema

import (
	"encoding/json"
	"testing"
)

var benchSchema = Schema{
	Name: "bench",
	Columns: []Column{
		{Name: "event_id", Path: "id", LogicalType: TypeString, Required: true},
		{Name: "event_type", Path: "type", LogicalType: TypeString, Required: true},
		{Name: "event_time", Path: "time", LogicalType: TypeTimestamp, Required: true},
		{Name: "source", Path: "source", LogicalType: TypeString, Required: true},
		{Name: "request_id", Path: "data.request_id", LogicalType: TypeString, Required: true},
		{Name: "provider", Path: "data.provider", LogicalType: TypeString, Required: true},
		{Name: "model", Path: "data.model", LogicalType: TypeString, Required: true},
		{Name: "status", Path: "data.status", LogicalType: TypeString, Required: true},
		{Name: "input_tokens", Path: "data.input_tokens", LogicalType: TypeInt64, Default: 0},
		{Name: "output_tokens", Path: "data.output_tokens", LogicalType: TypeInt64, Default: 0},
		{Name: "total_tokens", Path: "data.total_tokens", LogicalType: TypeInt64, Default: 0},
		{Name: "latency_ms", Path: "data.latency_ms", LogicalType: TypeInt64, Default: 0},
		{Name: "temperature", Path: "data.temperature", LogicalType: TypeFloat64, Default: 0.0},
		{Name: "user_id", Path: "data.user_id", LogicalType: TypeString, Default: ""},
		{Name: "org_id", Path: "data.org_id", LogicalType: TypeString, Default: ""},
		{Name: "environment", Path: "data.environment", LogicalType: TypeString, Default: "unknown"},
		{Name: "stream", Path: "data.stream", LogicalType: TypeBool, Default: false},
	},
}

var benchJSON = []byte(`{
	"specversion": "1.0",
	"id": "evt-000002-48cb76",
	"source": "/quanta/ai/openai/quanta-ai-gateway",
	"type": "ai.quanta.streaming_response",
	"subject": "gpt-4o",
	"datacontenttype": "application/json",
	"time": "2026-04-28T02:04:00Z",
	"data": {
		"request_id": "req-000002-25723c2a",
		"provider": "openai",
		"model": "gpt-4o",
		"status": "content_filtered",
		"status_class": "filtered",
		"origin": "chatbot",
		"input_tokens": 473,
		"output_tokens": 1873,
		"total_tokens": 2346,
		"latency_ms": 2067,
		"temperature": 0.85,
		"max_tokens": 512,
		"stream": false,
		"finish_reason": "stop",
		"user_id": "usr-014858",
		"org_id": "org-initech",
		"app_name": "quanta-ai-gateway",
		"app_version": "2.2.5",
		"environment": "canary"
	},
	"aiprovider": "openai",
	"environment": "canary",
	"statusclass": "filtered"
}`)

func BenchmarkMapper_Extract(b *testing.B) {
	m := NewMapper(benchSchema)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := m.Extract(benchJSON)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMapper_ExtractFrom(b *testing.B) {
	m := NewMapper(benchSchema)

	var obj map[string]any
	if err := json.Unmarshal(benchJSON, &obj); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := m.ExtractFrom(obj)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMapper_ExtractValues(b *testing.B) {
	m := NewMapper(benchSchema)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := m.ExtractValues(benchJSON)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMapper_NewMapper(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		_ = NewMapper(benchSchema)
	}
}

func BenchmarkJSON_Unmarshal(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		var obj map[string]any
		if err := json.Unmarshal(benchJSON, &obj); err != nil {
			b.Fatal(err)
		}
	}
}
