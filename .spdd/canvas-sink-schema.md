# REASONS Canvas: sink/schema

## Requirements

### Problem Statement
OLAP sinks (ClickHouse, DuckDB) need to map JSON event data to typed database columns. 
Current approach would require each sink to implement its own JSON parsing and type coercion.
We need a shared, declarative schema mapping layer aligned with **industry standards**.

### Industry Alignment: Open Data Contract Standard (ODCS)
Reference: https://bitol-io.github.io/open-data-contract-standard/v3.1.0/schema/

**ODCS Key Concepts Adopted:**
| ODCS Term | Quanta Mapping | Purpose |
|-----------|----------------|---------|
| `kind: DataContract` | `kind: Schema` | Document type identifier |
| `apiVersion` | `apiVersion` | Schema format version |
| `name` | `name` | Schema identifier |
| `description.purpose` | `description` | Business context |
| `schema[].properties` | `columns` | Column definitions |
| `logicalType` | `type` | Business type (string, int64) |
| `physicalType` | `physicalType` | Database-specific type |
| `businessName` | `businessName` | Human-readable name |
| `required` | `required` | Null handling |
| `classification` | `classification` | Data sensitivity |
| `tags` | `tags` | Categorization |

### Business Context
- Events are CloudEvents with nested `data` payload
- Schemas are **data contracts** between producers and consumers
- Users define column mappings in standalone YAML files
- Mappings are **business declarations**: "this JSON path becomes this column with this type"
- No code changes needed to add new columns or change types

### Acceptance Criteria
- [ ] ODCS-aligned YAML schema format
- [ ] Support JSONPath extraction from nested structures
- [ ] Logical types: string, int64, float64, bool, timestamp
- [ ] Physical types: database-specific (varchar, DateTime64, etc.)
- [ ] Handle missing values with configurable defaults
- [ ] Validate schema at startup (fail fast)
- [ ] Zero-allocation hot path for extraction

### Definition of Done
- [ ] `sink/schema` package with Mapper type
- [ ] Schema parsing with ODCS-compatible format
- [ ] Extraction benchmarks <100ns per field
- [ ] Unit tests for all type coercions
- [ ] Example schema file in `topology/schemas/`

---

## Entities

### Core Types

```go
// Schema represents a data contract for JSON-to-column mapping.
// Follows Open Data Contract Standard (ODCS) conventions.
type Schema struct {
    Kind        string   `yaml:"kind"`        // "Schema"
    APIVersion  string   `yaml:"apiVersion"`  // "v1"
    Name        string   `yaml:"name"`        // Unique identifier
    Description string   `yaml:"description"` // Business purpose
    Domain      string   `yaml:"domain"`      // Business domain
    Owner       string   `yaml:"owner"`       // Team/person responsible
    Tags        []string `yaml:"tags"`        // Categorization
    Columns     []Column `yaml:"columns"`     // Property definitions
}

// Column defines a single JSON-to-column mapping.
// Terminology aligned with ODCS "properties".
type Column struct {
    // Identity
    Name         string `yaml:"name"`         // Logical column name
    BusinessName string `yaml:"businessName"` // Human-readable name
    Description  string `yaml:"description"`  // Column purpose
    
    // Mapping
    Path string `yaml:"path"` // JSONPath to source (e.g., "data.provider")
    
    // Types
    LogicalType  string `yaml:"type"`         // Business type: string, int64, float64, bool, timestamp
    PhysicalType string `yaml:"physicalType"` // DB type: varchar(255), DateTime64, etc.
    
    // Constraints
    Required bool `yaml:"required"` // True = error if missing
    Default  any  `yaml:"default"`  // Default when missing
    
    // Governance
    Classification string   `yaml:"classification"` // public, internal, confidential, restricted
    Tags           []string `yaml:"tags"`           // Column-level tags
    
    // Lineage (optional)
    TransformLogic string `yaml:"transformLogic,omitempty"` // How value is derived
}

// LogicalType enumerates supported business types.
type LogicalType int

const (
    TypeString LogicalType = iota
    TypeInt64
    TypeFloat64
    TypeBool
    TypeTimestamp
)

// Row holds extracted column values.
type Row map[string]any

// Mapper extracts typed values from JSON according to schema.
type Mapper struct {
    schema Schema
    paths  [][]string // pre-split paths for performance
}
```

### Schema File Format (ODCS-Aligned)

```yaml
# topology/schemas/ai_events.schema.yaml
# Data Contract: AI Gateway Events
# Aligned with Open Data Contract Standard (ODCS) v3.1.0

kind: Schema
apiVersion: v1
name: ai_events
description: AI gateway CloudEvents with token usage and latency metrics
domain: ai-platform
owner: platform-team
tags:
  - streaming
  - ai
  - metrics

columns:
  # CloudEvents envelope (standard fields)
  - name: event_id
    businessName: Event Identifier
    description: Unique CloudEvents ID
    path: id
    type: string
    physicalType: String
    required: true
    classification: internal
    
  - name: event_type
    businessName: Event Type
    description: CloudEvents type attribute
    path: type
    type: string
    physicalType: LowCardinality(String)
    required: true
    classification: public
    
  - name: event_time
    businessName: Event Timestamp
    description: When the event occurred (RFC3339)
    path: time
    type: timestamp
    physicalType: DateTime64(3)
    required: true
    classification: public
    
  - name: event_source
    businessName: Event Source
    description: CloudEvents source URI
    path: source
    type: string
    physicalType: String
    required: true
    classification: internal

  # Business data (nested in data.*)
  - name: request_id
    businessName: Request ID
    description: Unique request identifier for tracing
    path: data.request_id
    type: string
    physicalType: String
    required: true
    classification: internal
    tags:
      - tracing
    
  - name: provider
    businessName: AI Provider
    description: LLM provider name (openai, anthropic, etc.)
    path: data.provider
    type: string
    physicalType: LowCardinality(String)
    required: true
    classification: public
    
  - name: model
    businessName: Model Name
    description: Model identifier used for inference
    path: data.model
    type: string
    physicalType: LowCardinality(String)
    required: true
    classification: public
    
  - name: status
    businessName: Response Status
    description: Request outcome status
    path: data.status
    type: string
    physicalType: LowCardinality(String)
    required: true
    classification: public
    
  - name: status_class
    businessName: Status Classification
    description: Grouped status category
    path: data.status_class
    type: string
    physicalType: LowCardinality(String)
    default: "unknown"
    classification: public
    
  - name: input_tokens
    businessName: Input Token Count
    description: Number of tokens in the prompt
    path: data.input_tokens
    type: int64
    physicalType: UInt32
    default: 0
    classification: internal
    tags:
      - metric
      - billing
    
  - name: output_tokens
    businessName: Output Token Count
    description: Number of tokens in the response
    path: data.output_tokens
    type: int64
    physicalType: UInt32
    default: 0
    classification: internal
    tags:
      - metric
      - billing
    
  - name: total_tokens
    businessName: Total Token Count
    description: Sum of input and output tokens
    path: data.total_tokens
    type: int64
    physicalType: UInt32
    default: 0
    classification: internal
    transformLogic: input_tokens + output_tokens
    tags:
      - metric
      - billing
    
  - name: latency_ms
    businessName: Request Latency
    description: End-to-end latency in milliseconds
    path: data.latency_ms
    type: int64
    physicalType: UInt32
    default: 0
    classification: internal
    tags:
      - metric
      - slo
    
  - name: temperature
    businessName: Temperature Setting
    description: Sampling temperature used
    path: data.temperature
    type: float64
    physicalType: Float32
    default: 0.0
    classification: public
    
  - name: user_id
    businessName: User Identifier
    description: User who made the request
    path: data.user_id
    type: string
    physicalType: String
    default: ""
    classification: confidential
    tags:
      - pii
    
  - name: org_id
    businessName: Organization ID
    description: Organization of the user
    path: data.org_id
    type: string
    physicalType: LowCardinality(String)
    default: ""
    classification: internal
    
  - name: environment
    businessName: Environment
    description: Deployment environment (prod, staging, canary)
    path: data.environment
    type: string
    physicalType: LowCardinality(String)
    default: "unknown"
    classification: public
    
  - name: stream_enabled
    businessName: Streaming Enabled
    description: Whether streaming was used
    path: data.stream
    type: bool
    physicalType: UInt8
    default: false
    classification: public
```

### Pipeline Config Reference

```yaml
# topology/pipeline.docker.clickhouse.yml
schema_version: v1

# Schema files to load (relative to topology dir)
schemas:
  - schemas/ai_events.schema.yaml

source:
  kind: kafka
  driver: sarama
  config: kafka_source.docker.yml

sinks:
  - clickhouse

sink_configs:
  clickhouse:
    address: "clickhouse:9000"
    database: analytics
    table: ai_events
    schema: ai_events           # Reference schema by name
    batch_size: 5000
    flush_interval: 10s
```

### Relationships
```
Schema 1──* Column     "schema contains ordered columns"
Mapper 1──1 Schema     "mapper uses schema for extraction"
Mapper 1──* Row        "mapper produces rows from JSON"
Pipeline *──* Schema   "pipeline loads and references schemas"
Sink *──1 Schema       "sink uses one schema for extraction"
```

---

## Approach

### Strategy: ODCS-Lite for Streaming

Full ODCS is designed for data catalogs and governance. We adopt a **streaming-optimized subset**:

| ODCS Feature | Quanta Support | Reason |
|--------------|----------------|--------|
| `schema.properties` | ✅ `columns` | Core mapping |
| `logicalType` | ✅ `type` | Business types |
| `physicalType` | ✅ `physicalType` | DB-specific |
| `businessName` | ✅ `businessName` | Documentation |
| `classification` | ✅ `classification` | Governance |
| `tags` | ✅ `tags` | Categorization |
| `required` | ✅ `required` | Null handling |
| `quality` rules | ❌ Deferred | Future enhancement |
| `relationships` | ❌ Deferred | Not needed for sink |
| `servers` | ❌ N/A | Sink config handles this |

### Compile-Once, Extract-Many

1. **Parse schema once** at startup → validate all paths and types
2. **Pre-compile paths** → split "data.provider" into ["data", "provider"]
3. **Extract hot path** → traverse JSON using pre-compiled paths
4. **Type coercion** → convert JSON values to target Go types

### Why NOT Full JSONPath?
- Full JSONPath (with filters, wildcards) is overkill
- Simple dot notation covers 99% of use cases
- Avoids external dependency
- Easier to optimize for streaming

### Concurrency Model
**Sequential** - Mapper is stateless after construction, safe for concurrent use.
Each Extract call is independent.

---

## Structure

```
sink/
└── schema/
    ├── doc.go           # Package documentation
    ├── types.go         # LogicalType enum, Column, Schema, Row
    ├── parse.go         # YAML parsing, validation
    ├── mapper.go        # Mapper with Extract method
    ├── coerce.go        # Type coercion functions
    ├── types_test.go    # Type enum tests
    ├── parse_test.go    # Parsing tests
    ├── mapper_test.go   # Extraction tests
    ├── coerce_test.go   # Coercion tests
    └── bench_test.go    # Performance benchmarks

topology/
└── schemas/
    └── ai_events.schema.yaml  # Example schema file
```

### Dependencies
- `encoding/json` - JSON unmarshaling
- `gopkg.in/yaml.v3` - YAML parsing (already in project)
- `time` - timestamp parsing
- `fmt` - error formatting
- `os` - file reading

---

## Operations

### Op 1: Define Types (types.go)

```go
// Package schema provides ODCS-aligned JSON-to-column mapping for OLAP sinks.
package schema

import "time"

// LogicalType represents the business type for column values.
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
    Kind        string   // "Schema"
    APIVersion  string   // "v1"
    Name        string   // Unique identifier
    Description string   // Business purpose
    Domain      string   // Business domain
    Owner       string   // Responsible team
    Tags        []string // Categorization
    Columns     []Column // Column definitions
}

// Column defines a single JSON-to-column mapping.
type Column struct {
    // Identity
    Name         string // Logical column name
    BusinessName string // Human-readable name
    Description  string // Column purpose
    
    // Mapping
    Path string // JSONPath to source
    
    // Types
    LogicalType  LogicalType // Business type
    PhysicalType string      // DB-specific type
    
    // Constraints
    Required bool // Error if missing
    Default  any  // Default when missing
    
    // Governance
    Classification string   // Data sensitivity
    Tags           []string // Column-level tags
}

// Row holds extracted column values keyed by column name.
type Row map[string]any
```

### Op 2: Schema Parsing (parse.go)

```go
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

    // Validate default matches type
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
```

### Op 3: Mapper Implementation (mapper.go)

```go
package schema

import (
    "encoding/json"
    "fmt"
    "strings"
)

// Mapper extracts typed values from JSON according to schema.
type Mapper struct {
    schema Schema
    paths  [][]string // pre-split paths
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

// ExtractValues returns values in column order (for batch inserts).
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
```

### Op 4: Type Coercion (coerce.go)

```go
package schema

import (
    "fmt"
    "time"
)

func coerce(val any, target LogicalType) (any, error) {
    if val == nil {
        return nil, nil
    }

    switch target {
    case TypeString:
        return coerceString(val)
    case TypeInt64:
        return coerceInt64(val)
    case TypeFloat64:
        return coerceFloat64(val)
    case TypeBool:
        return coerceBool(val)
    case TypeTimestamp:
        return coerceTimestamp(val)
    default:
        return nil, fmt.Errorf("unsupported type %v", target)
    }
}

func coerceString(val any) (string, error) {
    switch v := val.(type) {
    case string:
        return v, nil
    default:
        return fmt.Sprintf("%v", v), nil
    }
}

func coerceInt64(val any) (int64, error) {
    switch v := val.(type) {
    case float64:
        return int64(v), nil
    case int:
        return int64(v), nil
    case int64:
        return v, nil
    default:
        return 0, fmt.Errorf("cannot convert %T to int64", val)
    }
}

func coerceFloat64(val any) (float64, error) {
    switch v := val.(type) {
    case float64:
        return v, nil
    case int:
        return float64(v), nil
    case int64:
        return float64(v), nil
    default:
        return 0, fmt.Errorf("cannot convert %T to float64", val)
    }
}

func coerceBool(val any) (bool, error) {
    switch v := val.(type) {
    case bool:
        return v, nil
    case string:
        switch v {
        case "true", "1", "yes":
            return true, nil
        case "false", "0", "no", "":
            return false, nil
        default:
            return false, fmt.Errorf("cannot convert string %q to bool", v)
        }
    case float64:
        return v != 0, nil
    default:
        return false, fmt.Errorf("cannot convert %T to bool", val)
    }
}

func coerceTimestamp(val any) (time.Time, error) {
    switch v := val.(type) {
    case string:
        // RFC3339 (CloudEvents standard)
        t, err := time.Parse(time.RFC3339, v)
        if err == nil {
            return t, nil
        }
        // RFC3339Nano
        t, err = time.Parse(time.RFC3339Nano, v)
        if err == nil {
            return t, nil
        }
        return time.Time{}, fmt.Errorf("cannot parse timestamp %q", v)
    case float64:
        // Unix seconds
        return time.Unix(int64(v), 0), nil
    default:
        return time.Time{}, fmt.Errorf("cannot convert %T to timestamp", val)
    }
}
```

---

## Norms

### ODCS Alignment
- Schema files use ODCS-compatible field names where applicable
- `kind: Schema` mirrors `kind: DataContract`
- `columns` maps to ODCS `properties`
- `type` (logicalType) + `physicalType` dual typing
- `classification` for governance
- `businessName` for human readability

### Discovered Conventions (from quanta codebase)
- Package doc in `doc.go`
- Enum types with String() method
- Config types separate from domain types
- Validate at construction time
- Error messages: "package: context: detail"

### Config Philosophy
- **Declarative**: Columns are business declarations, not code
- **Explicit**: No magic field inference
- **Fail-fast**: Invalid schema = startup error
- **Typed**: Every column has explicit logical + physical type

---

## Safeguards

### Invariants
1. All column names are unique within a schema
2. All paths are non-empty
3. All logical types are valid
4. Required fields with missing values cause extraction error
5. Default values match declared types

### Boundaries
- Maximum 256 columns per schema
- Maximum path depth: 10 levels
- JSON input must be valid object

### Governance Support
| Classification | Meaning | Example Fields |
|----------------|---------|----------------|
| public | Safe to expose | event_type, model |
| internal | Internal use only | request_id, latency_ms |
| confidential | Requires access control | user_id, org_id |
| restricted | PII/sensitive | (future: billing info) |
