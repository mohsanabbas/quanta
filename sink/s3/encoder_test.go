package s3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		give    string
		wantErr bool
	}{
		{name: "jsonl", give: "jsonl", wantErr: false},
		{name: "unknown", give: "parquet", wantErr: true},
		{name: "empty", give: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := newEncoder(tt.give)
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
