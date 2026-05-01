package schema

// LogicalType represents business types for column values.
type LogicalType int

const (
	TypeString LogicalType = iota
	TypeInt64
	TypeFloat64
	TypeBool
	TypeTimestamp
)

func (t LogicalType) String() string {
	switch t {
	case TypeString:
		return "string"
	case TypeInt64:
		return "int64"
	case TypeFloat64:
		return "float64"
	case TypeBool:
		return "bool"
	case TypeTimestamp:
		return "timestamp"
	default:
		return "unknown"
	}
}

// ParseLogicalType converts string to LogicalType.
func ParseLogicalType(s string) (LogicalType, bool) {
	switch s {
	case "string":
		return TypeString, true
	case "int64":
		return TypeInt64, true
	case "float64":
		return TypeFloat64, true
	case "bool":
		return TypeBool, true
	case "timestamp":
		return TypeTimestamp, true
	default:
		return 0, false
	}
}

// Classification levels for data governance.
const (
	ClassPublic       = "public"
	ClassInternal     = "internal"
	ClassConfidential = "confidential"
	ClassRestricted   = "restricted"
)

// Schema represents a data contract for JSON-to-column mapping.
type Schema struct {
	Kind        string
	APIVersion  string
	Name        string
	Description string
	Domain      string
	Owner       string
	Tags        []string
	Columns     []Column
}

// Column defines a single JSON-to-column mapping.
type Column struct {
	Name           string
	BusinessName   string
	Description    string
	Path           string
	LogicalType    LogicalType
	PhysicalType   string
	Required       bool
	Default        any
	Classification string
	Tags           []string
}

// Row holds extracted column values keyed by column name.
type Row map[string]any
