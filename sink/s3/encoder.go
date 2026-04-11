package s3

import (
	"bytes"
	"errors"
)

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

func newEncoder(name string) (Encoder, error) {
	switch name {
	case "jsonl":
		return jsonlEncoder{}, nil
	default:
		return nil, errors.New("unsupported encoder format")
	}
}
