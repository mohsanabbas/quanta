package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSchema_Valid(t *testing.T) {
	yaml := `
kind: Schema
apiVersion: v1
name: test_events
description: Test schema
domain: testing
owner: test-team
tags:
  - test
columns:
  - name: event_id
    path: id
    type: string
    required: true
  - name: count
    path: data.count
    type: int64
    default: 0
`
	schema, err := ParseSchema([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseSchema() error = %v", err)
	}

	if schema.Name != "test_events" {
		t.Errorf("Name = %q, want %q", schema.Name, "test_events")
	}
	if schema.Kind != "Schema" {
		t.Errorf("Kind = %q, want %q", schema.Kind, "Schema")
	}
	if len(schema.Columns) != 2 {
		t.Fatalf("len(Columns) = %d, want 2", len(schema.Columns))
	}

	col := schema.Columns[0]
	if col.Name != "event_id" {
		t.Errorf("Columns[0].Name = %q, want %q", col.Name, "event_id")
	}
	if col.LogicalType != TypeString {
		t.Errorf("Columns[0].LogicalType = %v, want %v", col.LogicalType, TypeString)
	}
	if !col.Required {
		t.Error("Columns[0].Required = false, want true")
	}
}

func TestParseSchema_Errors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "empty name",
			yaml: `kind: Schema
columns:
  - name: id
    path: id
    type: string`,
			want: "name is required",
		},
		{
			name: "no columns",
			yaml: `kind: Schema
name: test
columns: []`,
			want: "no columns defined",
		},
		{
			name: "empty column name",
			yaml: `kind: Schema
name: test
columns:
  - path: id
    type: string`,
			want: "empty name",
		},
		{
			name: "empty path",
			yaml: `kind: Schema
name: test
columns:
  - name: id
    type: string`,
			want: "empty path",
		},
		{
			name: "invalid type",
			yaml: `kind: Schema
name: test
columns:
  - name: id
    path: id
    type: invalid`,
			want: "invalid type",
		},
		{
			name: "duplicate column",
			yaml: `kind: Schema
name: test
columns:
  - name: id
    path: id
    type: string
  - name: id
    path: other
    type: string`,
			want: "duplicate column",
		},
		{
			name: "wrong kind",
			yaml: `kind: DataContract
name: test
columns:
  - name: id
    path: id
    type: string`,
			want: "expected kind=Schema",
		},
		{
			name: "invalid default type",
			yaml: `kind: Schema
name: test
columns:
  - name: count
    path: count
    type: int64
    default: "not a number"`,
			want: "default must be integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSchema([]byte(tt.yaml))
			if err == nil {
				t.Fatal("ParseSchema() expected error, got nil")
			}
			if !contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.want)
			}
		})
	}
}

func TestLoadSchema(t *testing.T) {
	yaml := `kind: Schema
apiVersion: v1
name: file_test
columns:
  - name: id
    path: id
    type: string
    required: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.schema.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	schema, err := LoadSchema(path)
	if err != nil {
		t.Fatalf("LoadSchema() error = %v", err)
	}
	if schema.Name != "file_test" {
		t.Errorf("Name = %q, want %q", schema.Name, "file_test")
	}
}

func TestLoadSchema_NotFound(t *testing.T) {
	_, err := LoadSchema("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("LoadSchema() expected error for missing file")
	}
}

func TestValidateDefault_AllTypes(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "string default",
			yaml: `kind: Schema
name: test
columns:
  - name: s
    path: s
    type: string
    default: "hello"`,
			wantErr: false,
		},
		{
			name: "int64 default",
			yaml: `kind: Schema
name: test
columns:
  - name: n
    path: n
    type: int64
    default: 42`,
			wantErr: false,
		},
		{
			name: "float64 default",
			yaml: `kind: Schema
name: test
columns:
  - name: f
    path: f
    type: float64
    default: 3.14`,
			wantErr: false,
		},
		{
			name: "bool default",
			yaml: `kind: Schema
name: test
columns:
  - name: b
    path: b
    type: bool
    default: true`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSchema([]byte(tt.yaml))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchSubstring(s, substr)))
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
