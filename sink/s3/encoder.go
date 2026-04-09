package s3

import (
	"bytes"
	"fmt"
	"io"
)

type Encoder interface {
	Encode(records [][]byte) (io.Reader, error)
	ContentType() string
}

type jsonlEncoder struct{}

func (jsonlEncoder) Encode(records [][]byte) (io.Reader, error) {
	if len(records) == 0 {
		return bytes.NewReader(nil), nil
	}

	joined := bytes.Join(records, []byte("\n"))
	joined = append(joined, '\n')
	return bytes.NewReader(joined), nil
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
