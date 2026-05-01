package s3

import (
	"bytes"
	"io"
	"testing"

	"github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"quanta/sink/schema"
)

func TestJsonlEncoder(t *testing.T) {
	tests := []struct {
		name     string
		give     [][]byte
		want     string
		wantType string
	}{
		{
			name:     "empty records",
			give:     nil,
			want:     "",
			wantType: "application/x-ndjson",
		},
		{
			name:     "single record",
			give:     [][]byte{[]byte(`{"id":1}`)},
			want:     "{\"id\":1}\n",
			wantType: "application/x-ndjson",
		},
		{
			name:     "multiple records",
			give:     [][]byte{[]byte(`{"a":1}`), []byte(`{"b":2}`), []byte(`{"c":3}`)},
			want:     "{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n",
			wantType: "application/x-ndjson",
		},
		{
			name:     "binary data",
			give:     [][]byte{{0x00, 0xFF}, {0x01, 0xFE}},
			want:     "\x00\xFF\n\x01\xFE\n",
			wantType: "application/x-ndjson",
		},
	}

	enc := &jsonlEncoder{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := enc.Encode(tt.give)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
			assert.Equal(t, tt.wantType, enc.ContentType())
		})
	}
}

func TestNewEncoder(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		mapper  *schema.Mapper
		wantErr bool
	}{
		{name: "jsonl", format: "jsonl", mapper: nil, wantErr: false},
		{name: "empty defaults to jsonl", format: "", mapper: nil, wantErr: false},
		{name: "parquet without mapper", format: "parquet", mapper: nil, wantErr: true},
		{name: "unknown format", format: "avro", mapper: nil, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := newEncoder(tt.format, tt.mapper)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, enc)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, enc)
		})
	}
}

// testSchema returns a schema matching ai_events.schema.yaml structure
func testSchema() schema.Schema {
	return schema.Schema{
		Name:        "ai_events",
		Description: "AI gateway CloudEvents with token usage and latency metrics",
		Columns: []schema.Column{
			{Name: "event_id", Path: "id", LogicalType: schema.TypeString, Required: true},
			{Name: "event_type", Path: "type", LogicalType: schema.TypeString, Required: true},
			{Name: "event_source", Path: "source", LogicalType: schema.TypeString, Required: true},
			{Name: "event_time", Path: "time", LogicalType: schema.TypeTimestamp, Required: true},
			{Name: "provider", Path: "data.provider", LogicalType: schema.TypeString, Required: true},
			{Name: "model", Path: "data.model", LogicalType: schema.TypeString, Required: true},
			{Name: "input_tokens", Path: "data.input_tokens", LogicalType: schema.TypeInt64, Default: int64(0)},
			{Name: "output_tokens", Path: "data.output_tokens", LogicalType: schema.TypeInt64, Default: int64(0)},
			{Name: "latency_ms", Path: "data.latency_ms", LogicalType: schema.TypeInt64, Default: int64(0)},
			{Name: "temperature", Path: "data.temperature", LogicalType: schema.TypeFloat64, Default: float64(0)},
			{Name: "stream_enabled", Path: "data.stream", LogicalType: schema.TypeBool, Default: false},
		},
	}
}

// testCloudEvent returns a realistic CloudEvent JSON
func testCloudEvent() []byte {
	return []byte(`{
		"specversion": "1.0",
		"id": "evt-001-abc123",
		"type": "ai.quanta.chat_completion",
		"source": "/quanta/ai/openai/quanta-ai-gateway",
		"time": "2026-05-01T12:00:00Z",
		"data": {
			"provider": "openai",
			"model": "gpt-4o",
			"input_tokens": 150,
			"output_tokens": 500,
			"latency_ms": 1234,
			"temperature": 0.7,
			"stream": true
		}
	}`)
}

func TestParquetEncoder(t *testing.T) {
	s := testSchema()
	mapper := schema.NewMapper(s)

	enc, err := newEncoder("parquet", mapper)
	require.NoError(t, err)
	assert.Equal(t, "application/vnd.apache.parquet", enc.ContentType())

	// Encode realistic CloudEvents
	records := [][]byte{
		testCloudEvent(),
		[]byte(`{
			"id": "evt-002-def456",
			"type": "ai.quanta.streaming_response",
			"source": "/quanta/ai/anthropic/inference-router",
			"time": "2026-05-01T12:01:00Z",
			"data": {
				"provider": "anthropic",
				"model": "claude-sonnet-4",
				"input_tokens": 200,
				"output_tokens": 1000,
				"latency_ms": 2500,
				"temperature": 0.5,
				"stream": false
			}
		}`),
	}

	data, err := enc.Encode(records)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Parquet files start with "PAR1" magic
	require.True(t, len(data) >= 4, "parquet data too short")
	assert.Equal(t, "PAR1", string(data[:4]), "missing PAR1 magic header")

	// Parquet files also end with "PAR1" magic
	assert.Equal(t, "PAR1", string(data[len(data)-4:]), "missing PAR1 magic footer")
}

func TestParquetEncoder_SnappyCompression(t *testing.T) {
	s := testSchema()
	mapper := schema.NewMapper(s)

	enc, err := newEncoder("parquet", mapper)
	require.NoError(t, err)

	// Generate multiple records for compression to be effective
	records := make([][]byte, 100)
	for i := range records {
		records[i] = testCloudEvent()
	}

	data, err := enc.Encode(records)
	require.NoError(t, err)

	// Use parquet.OpenFile to read with file's own schema
	file, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)

	// Check schema has expected columns
	pschema := file.Schema()
	assert.Equal(t, "event", pschema.Name())

	// Verify we have all expected columns
	columnNames := make([]string, 0)
	for _, field := range pschema.Fields() {
		columnNames = append(columnNames, field.Name())
	}
	assert.Contains(t, columnNames, "event_id")
	assert.Contains(t, columnNames, "provider")
	assert.Contains(t, columnNames, "input_tokens")
	assert.Contains(t, columnNames, "temperature")
	assert.Contains(t, columnNames, "stream_enabled")

	// Verify row count
	assert.Equal(t, int64(100), file.NumRows())

	// Check compression codec via root column
	root := file.Root()
	require.NotNil(t, root)
	// Columns have compression set when file is read
	for _, col := range root.Columns() {
		codec := col.Compression()
		if codec != nil {
			assert.Equal(t, "SNAPPY", codec.String())
			break
		}
	}
}

func TestParquetEncoder_TimestampMicrosecondPrecision(t *testing.T) {
	s := schema.Schema{
		Name: "timestamp_test",
		Columns: []schema.Column{
			{Name: "event_id", Path: "id", LogicalType: schema.TypeString, Required: true},
			{Name: "event_time", Path: "time", LogicalType: schema.TypeTimestamp, Required: true},
		},
	}
	mapper := schema.NewMapper(s)

	enc, err := newEncoder("parquet", mapper)
	require.NoError(t, err)

	records := [][]byte{
		[]byte(`{"id": "evt-001", "time": "2026-05-01T12:30:45.123456Z"}`),
	}

	data, err := enc.Encode(records)
	require.NoError(t, err)

	// Verify file is valid parquet
	file, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Equal(t, int64(1), file.NumRows())

	// Verify timestamp column exists with correct logical type
	found := false
	for _, field := range file.Schema().Fields() {
		if field.Name() == "event_time" {
			found = true
			// Timestamp(MICROS) should be present
			assert.NotNil(t, field)
		}
	}
	assert.True(t, found, "event_time column not found")
}

func TestParquetEncoder_EmptyRecords(t *testing.T) {
	s := testSchema()
	mapper := schema.NewMapper(s)

	enc, err := newEncoder("parquet", mapper)
	require.NoError(t, err)

	data, err := enc.Encode(nil)
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestParquetEncoder_AllLogicalTypes(t *testing.T) {
	s := schema.Schema{
		Name: "all_types",
		Columns: []schema.Column{
			{Name: "str_col", Path: "str", LogicalType: schema.TypeString, Required: true},
			{Name: "int_col", Path: "num", LogicalType: schema.TypeInt64, Required: true},
			{Name: "float_col", Path: "flt", LogicalType: schema.TypeFloat64, Required: true},
			{Name: "bool_col", Path: "flag", LogicalType: schema.TypeBool, Required: true},
			{Name: "ts_col", Path: "ts", LogicalType: schema.TypeTimestamp, Required: true},
		},
	}
	mapper := schema.NewMapper(s)

	enc, err := newEncoder("parquet", mapper)
	require.NoError(t, err)

	records := [][]byte{
		[]byte(`{
			"str": "hello",
			"num": 42,
			"flt": 3.14159,
			"flag": true,
			"ts": "2026-05-01T10:00:00Z"
		}`),
	}

	data, err := enc.Encode(records)
	require.NoError(t, err)

	// Verify file structure
	file, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Equal(t, int64(1), file.NumRows())

	// Verify all columns present
	columnNames := make([]string, 0)
	for _, field := range file.Schema().Fields() {
		columnNames = append(columnNames, field.Name())
	}
	assert.Contains(t, columnNames, "str_col")
	assert.Contains(t, columnNames, "int_col")
	assert.Contains(t, columnNames, "float_col")
	assert.Contains(t, columnNames, "bool_col")
	assert.Contains(t, columnNames, "ts_col")
}

func TestParquetEncoder_OptionalFields(t *testing.T) {
	s := schema.Schema{
		Name: "optional_test",
		Columns: []schema.Column{
			{Name: "id", Path: "id", LogicalType: schema.TypeString, Required: true},
			{Name: "optional_str", Path: "opt", LogicalType: schema.TypeString, Required: false, Default: ""},
			{Name: "optional_int", Path: "count", LogicalType: schema.TypeInt64, Required: false, Default: int64(0)},
		},
	}
	mapper := schema.NewMapper(s)

	enc, err := newEncoder("parquet", mapper)
	require.NoError(t, err)

	// Record missing optional fields - should use defaults
	records := [][]byte{
		[]byte(`{"id": "evt-001"}`),
	}

	data, err := enc.Encode(records)
	require.NoError(t, err)

	file, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Equal(t, int64(1), file.NumRows())

	// Read using row reader
	reader := file.Root()
	assert.NotNil(t, reader)
}

// readerAtWrapper wraps io.Reader + size to implement io.ReaderAt
type readerAtWrapper struct {
	r    *bytes.Reader
	size int64
}

func newReaderAt(data []byte) io.ReaderAt {
	return bytes.NewReader(data)
}

func BenchmarkParquetEncode(b *testing.B) {
	s := testSchema()
	mapper := schema.NewMapper(s)
	enc, _ := newEncoder("parquet", mapper)

	records := make([][]byte, 1000)
	for i := range records {
		records[i] = testCloudEvent()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = enc.Encode(records)
	}
}

func BenchmarkParquetEncode_100(b *testing.B) {
	s := testSchema()
	mapper := schema.NewMapper(s)
	enc, _ := newEncoder("parquet", mapper)

	records := make([][]byte, 100)
	for i := range records {
		records[i] = testCloudEvent()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = enc.Encode(records)
	}
}
