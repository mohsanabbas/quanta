package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Mapper extracts typed values from JSON according to schema.
type Mapper struct {
	schema Schema
	paths  [][]string
}

// NewMapper creates a mapper with pre-compiled paths.
func NewMapper(schema Schema) *Mapper {
	paths := make([][]string, len(schema.Columns))
	for i, col := range schema.Columns {
		paths[i] = strings.Split(col.Path, ".")
	}
	return &Mapper{schema: schema, paths: paths}
}

// Schema returns the mapper's schema.
func (m *Mapper) Schema() Schema {
	return m.schema
}

// ColumnNames returns ordered column names.
func (m *Mapper) ColumnNames() []string {
	names := make([]string, len(m.schema.Columns))
	for i, col := range m.schema.Columns {
		names[i] = col.Name
	}
	return names
}

// Extract parses JSON and extracts all columns into a Row.
func (m *Mapper) Extract(data []byte) (Row, error) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("schema: invalid JSON: %w", err)
	}
	return m.ExtractFrom(obj)
}

// ExtractFrom extracts columns from already-parsed JSON.
func (m *Mapper) ExtractFrom(obj map[string]any) (Row, error) {
	row := make(Row, len(m.schema.Columns))

	for i, col := range m.schema.Columns {
		raw, found := m.traverse(obj, m.paths[i])

		if !found {
			if col.Required {
				return nil, fmt.Errorf("schema: required field %q not found at path %q", col.Name, col.Path)
			}
			row[col.Name] = col.Default
			continue
		}

		val, err := coerce(raw, col.LogicalType)
		if err != nil {
			return nil, fmt.Errorf("schema: column %q: %w", col.Name, err)
		}
		row[col.Name] = val
	}

	return row, nil
}

// ExtractValues returns values in column order for batch inserts.
func (m *Mapper) ExtractValues(data []byte) ([]any, error) {
	row, err := m.Extract(data)
	if err != nil {
		return nil, err
	}
	vals := make([]any, len(m.schema.Columns))
	for i, col := range m.schema.Columns {
		vals[i] = row[col.Name]
	}
	return vals, nil
}

// ExtractValuesFrom extracts values from already-parsed JSON.
func (m *Mapper) ExtractValuesFrom(obj map[string]any) ([]any, error) {
	row, err := m.ExtractFrom(obj)
	if err != nil {
		return nil, err
	}
	vals := make([]any, len(m.schema.Columns))
	for i, col := range m.schema.Columns {
		vals[i] = row[col.Name]
	}
	return vals, nil
}

func (m *Mapper) traverse(obj map[string]any, path []string) (any, bool) {
	var current any = obj
	for _, key := range path {
		switch v := current.(type) {
		case map[string]any:
			val, ok := v[key]
			if !ok {
				return nil, false
			}
			current = val
		default:
			return nil, false
		}
	}
	return current, true
}
