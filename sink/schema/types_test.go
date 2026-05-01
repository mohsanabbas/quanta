package schema

import "testing"

func TestLogicalType_String(t *testing.T) {
	tests := []struct {
		lt   LogicalType
		want string
	}{
		{TypeString, "string"},
		{TypeInt64, "int64"},
		{TypeFloat64, "float64"},
		{TypeBool, "bool"},
		{TypeTimestamp, "timestamp"},
		{LogicalType(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.lt.String(); got != tt.want {
			t.Errorf("LogicalType(%d).String() = %q, want %q", tt.lt, got, tt.want)
		}
	}
}

func TestParseLogicalType(t *testing.T) {
	tests := []struct {
		input string
		want  LogicalType
		ok    bool
	}{
		{"string", TypeString, true},
		{"int64", TypeInt64, true},
		{"float64", TypeFloat64, true},
		{"bool", TypeBool, true},
		{"timestamp", TypeTimestamp, true},
		{"invalid", 0, false},
		{"", 0, false},
		{"STRING", 0, false}, // case sensitive
	}

	for _, tt := range tests {
		got, ok := ParseLogicalType(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseLogicalType(%q) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestClassificationConstants(t *testing.T) {
	if ClassPublic != "public" {
		t.Errorf("ClassPublic = %q, want %q", ClassPublic, "public")
	}
	if ClassInternal != "internal" {
		t.Errorf("ClassInternal = %q, want %q", ClassInternal, "internal")
	}
	if ClassConfidential != "confidential" {
		t.Errorf("ClassConfidential = %q, want %q", ClassConfidential, "confidential")
	}
	if ClassRestricted != "restricted" {
		t.Errorf("ClassRestricted = %q, want %q", ClassRestricted, "restricted")
	}
}
