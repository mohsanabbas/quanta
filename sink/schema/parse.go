package schema

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SchemaFile is the YAML structure for schema files.
type SchemaFile struct {
	Kind        string         `yaml:"kind"`
	APIVersion  string         `yaml:"apiVersion"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Domain      string         `yaml:"domain"`
	Owner       string         `yaml:"owner"`
	Tags        []string       `yaml:"tags"`
	Columns     []ColumnConfig `yaml:"columns"`
}

// ColumnConfig is the YAML structure for column definitions.
type ColumnConfig struct {
	Name           string   `yaml:"name"`
	BusinessName   string   `yaml:"businessName"`
	Description    string   `yaml:"description"`
	Path           string   `yaml:"path"`
	Type           string   `yaml:"type"`
	PhysicalType   string   `yaml:"physicalType"`
	Required       bool     `yaml:"required"`
	Default        any      `yaml:"default,omitempty"`
	Classification string   `yaml:"classification"`
	Tags           []string `yaml:"tags"`
}

// LoadSchema reads and parses a schema file.
func LoadSchema(path string) (Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Schema{}, fmt.Errorf("schema: read %s: %w", path, err)
	}
	return ParseSchema(data)
}

// ParseSchema validates and converts YAML bytes to Schema.
func ParseSchema(data []byte) (Schema, error) {
	var file SchemaFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return Schema{}, fmt.Errorf("schema: invalid YAML: %w", err)
	}
	return validateSchema(file)
}

func validateSchema(file SchemaFile) (Schema, error) {
	if file.Kind != "" && file.Kind != "Schema" {
		return Schema{}, fmt.Errorf("schema: expected kind=Schema, got %q", file.Kind)
	}
	if file.Name == "" {
		return Schema{}, fmt.Errorf("schema: name is required")
	}
	if len(file.Columns) == 0 {
		return Schema{}, fmt.Errorf("schema: no columns defined")
	}
	if len(file.Columns) > 256 {
		return Schema{}, fmt.Errorf("schema: too many columns (%d > 256)", len(file.Columns))
	}

	cols := make([]Column, 0, len(file.Columns))
	seen := make(map[string]bool)

	for i, cc := range file.Columns {
		col, err := validateColumn(i, cc, seen)
		if err != nil {
			return Schema{}, err
		}
		cols = append(cols, col)
		seen[cc.Name] = true
	}

	return Schema{
		Kind:        "Schema",
		APIVersion:  file.APIVersion,
		Name:        file.Name,
		Description: file.Description,
		Domain:      file.Domain,
		Owner:       file.Owner,
		Tags:        file.Tags,
		Columns:     cols,
	}, nil
}

func validateColumn(idx int, cc ColumnConfig, seen map[string]bool) (Column, error) {
	if cc.Name == "" {
		return Column{}, fmt.Errorf("schema: column %d has empty name", idx)
	}
	if seen[cc.Name] {
		return Column{}, fmt.Errorf("schema: duplicate column name %q", cc.Name)
	}
	if cc.Path == "" {
		return Column{}, fmt.Errorf("schema: column %q has empty path", cc.Name)
	}

	lt, ok := ParseLogicalType(cc.Type)
	if !ok {
		return Column{}, fmt.Errorf("schema: column %q has invalid type %q", cc.Name, cc.Type)
	}

	col := Column{
		Name:           cc.Name,
		BusinessName:   cc.BusinessName,
		Description:    cc.Description,
		Path:           cc.Path,
		LogicalType:    lt,
		PhysicalType:   cc.PhysicalType,
		Required:       cc.Required,
		Default:        cc.Default,
		Classification: cc.Classification,
		Tags:           cc.Tags,
	}

	if !col.Required && col.Default != nil {
		if err := validateDefault(col); err != nil {
			return Column{}, err
		}
	}

	return col, nil
}

func validateDefault(col Column) error {
	switch col.LogicalType {
	case TypeString:
		if _, ok := col.Default.(string); !ok {
			return fmt.Errorf("schema: column %q default must be string", col.Name)
		}
	case TypeInt64:
		switch col.Default.(type) {
		case int, int64, float64:
		default:
			return fmt.Errorf("schema: column %q default must be integer", col.Name)
		}
	case TypeFloat64:
		switch col.Default.(type) {
		case int, int64, float64:
		default:
			return fmt.Errorf("schema: column %q default must be number", col.Name)
		}
	case TypeBool:
		if _, ok := col.Default.(bool); !ok {
			return fmt.Errorf("schema: column %q default must be bool", col.Name)
		}
	case TypeTimestamp:
		// Timestamps rarely have defaults
	}
	return nil
}
