package s3

import (
	"bytes"
	"fmt"
)

// Encoder serializes a slice of raw records into a single body for S3 upload.
type Encoder interface {
	Encode(records [][]byte) ([]byte, error)
	ContentType() string
}

// jsonlEncoder encodes records as newline-delimited JSON (JSON Lines / NDJSON).
type jsonlEncoder struct{}

func (jsonlEncoder) Encode(records [][]byte) ([]byte, error) {
	if len(records) == 0 {
		return nil, nil
	}
	// JSONL spec: each record on its own line, terminated by \n.
	joined := bytes.Join(records, []byte("\n"))
	joined = append(joined, '\n')
	return joined, nil
}

func (jsonlEncoder) ContentType() string { return "application/x-ndjson" }

func newEncoder(name string) (Encoder, error) {
	switch name {
	case "jsonl":
		return jsonlEncoder{}, nil
	default:
		return nil, fmt.Errorf("unsupported format %q", name)
	}
}
