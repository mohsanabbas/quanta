package s3

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/snappy"

	"quanta/sink/schema"
)

// Encoder transforms records into a wire format for S3 storage.
type Encoder interface {
	Encode(records [][]byte) ([]byte, error)
	ContentType() string
}

type jsonlEncoder struct{}

func (jsonlEncoder) Encode(records [][]byte) ([]byte, error) {
	if len(records) == 0 {
		return nil, nil
	}
	joined := bytes.Join(records, []byte("\n"))
	joined = append(joined, '\n')
	return joined, nil
}

func (jsonlEncoder) ContentType() string { return "application/x-ndjson" }

// parquetEncoder writes records as Parquet using schema mapping.
// Uses Snappy compression for Spark/Databricks/BigQuery compatibility.
type parquetEncoder struct {
	mapper *schema.Mapper
	schema *parquet.Schema
}

func newParquetEncoder(mapper *schema.Mapper) (*parquetEncoder, error) {
	if mapper == nil {
		return nil, errors.New("parquet encoder requires schema mapper")
	}

	node := buildParquetSchema(mapper.Schema())
	ps := parquet.NewSchema("event", node)

	return &parquetEncoder{
		mapper: mapper,
		schema: ps,
	}, nil
}

func (e *parquetEncoder) Encode(records [][]byte) ([]byte, error) {
	if len(records) == 0 {
		return nil, nil
	}

	rows := make([]map[string]any, 0, len(records))
	for _, data := range records {
		row, err := e.mapper.Extract(data)
		if err != nil {
			return nil, fmt.Errorf("parquet: extract row: %w", err)
		}
		rows = append(rows, row)
	}

	var buf bytes.Buffer

	// Use Snappy compression - industry standard for Spark/Databricks/BigQuery
	// DataPageVersion 2 for better encoding support
	pw := parquet.NewGenericWriter[map[string]any](&buf, e.schema,
		parquet.Compression(&snappy.Codec{}),
		parquet.DataPageVersion(2),
	)

	if _, err := pw.Write(rows); err != nil {
		return nil, fmt.Errorf("parquet: write rows: %w", err)
	}
	if err := pw.Close(); err != nil {
		return nil, fmt.Errorf("parquet: close writer: %w", err)
	}

	return buf.Bytes(), nil
}

func (e *parquetEncoder) ContentType() string { return "application/vnd.apache.parquet" }

func buildParquetSchema(s schema.Schema) parquet.Group {
	group := make(parquet.Group)

	for _, col := range s.Columns {
		var node parquet.Node

		switch col.LogicalType {
		case schema.TypeString:
			node = parquet.String()
		case schema.TypeInt64:
			node = parquet.Int(64)
		case schema.TypeFloat64:
			node = parquet.Leaf(parquet.DoubleType)
		case schema.TypeBool:
			node = parquet.Leaf(parquet.BooleanType)
		case schema.TypeTimestamp:
			// Microsecond precision - standard for Spark/Hive/BigQuery
			node = parquet.Timestamp(parquet.Microsecond)
		default:
			node = parquet.String() // fallback
		}

		if !col.Required {
			node = parquet.Optional(node)
		}

		group[col.Name] = node
	}

	return group
}

func newEncoder(name string, mapper *schema.Mapper) (Encoder, error) {
	switch name {
	case "jsonl", "":
		return jsonlEncoder{}, nil
	case "parquet":
		return newParquetEncoder(mapper)
	default:
		return nil, errors.New("unsupported encoder format: " + name)
	}
}

// TimestampMicros converts time.Time to microseconds for Parquet.
func TimestampMicros(t time.Time) int64 {
	return t.UnixMicro()
}
