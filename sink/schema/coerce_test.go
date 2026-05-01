package schema

import (
	"testing"
	"time"
)

func TestCoerceString(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{"hello", "hello"},
		{123.0, "123"},
		{true, "true"},
		{nil, ""},
	}

	for _, tt := range tests {
		if tt.input == nil {
			got, err := coerce(nil, TypeString)
			if err != nil {
				t.Errorf("coerce(nil, TypeString) error = %v", err)
			}
			if got != nil {
				t.Errorf("coerce(nil, TypeString) = %v, want nil", got)
			}
			continue
		}
		got, err := coerceString(tt.input)
		if err != nil {
			t.Errorf("coerceString(%v) error = %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("coerceString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCoerceInt64(t *testing.T) {
	tests := []struct {
		input   any
		want    int64
		wantErr bool
	}{
		{float64(42), 42, false},
		{float64(3.9), 3, false}, // truncates
		{int(100), 100, false},
		{int64(200), 200, false},
		{"not a number", 0, true},
		{true, 0, true},
	}

	for _, tt := range tests {
		got, err := coerceInt64(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("coerceInt64(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("coerceInt64(%v) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCoerceFloat64(t *testing.T) {
	tests := []struct {
		input   any
		want    float64
		wantErr bool
	}{
		{float64(3.14), 3.14, false},
		{int(42), 42.0, false},
		{int64(100), 100.0, false},
		{"not a number", 0, true},
	}

	for _, tt := range tests {
		got, err := coerceFloat64(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("coerceFloat64(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("coerceFloat64(%v) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestCoerceBool(t *testing.T) {
	tests := []struct {
		input   any
		want    bool
		wantErr bool
	}{
		{true, true, false},
		{false, false, false},
		{"true", true, false},
		{"false", false, false},
		{"1", true, false},
		{"0", false, false},
		{"yes", true, false},
		{"no", false, false},
		{"", false, false},
		{float64(1), true, false},
		{float64(0), false, false},
		{"invalid", false, true},
		{[]int{}, false, true},
	}

	for _, tt := range tests {
		got, err := coerceBool(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("coerceBool(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("coerceBool(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestCoerceTimestamp(t *testing.T) {
	tests := []struct {
		input   any
		want    time.Time
		wantErr bool
	}{
		{
			"2026-04-28T02:04:00Z",
			time.Date(2026, 4, 28, 2, 4, 0, 0, time.UTC),
			false,
		},
		{
			"2026-04-28T02:04:00.123456789Z",
			time.Date(2026, 4, 28, 2, 4, 0, 123456789, time.UTC),
			false,
		},
		{
			float64(1714272240), // Unix timestamp
			time.Unix(1714272240, 0),
			false,
		},
		{"not a timestamp", time.Time{}, true},
		{true, time.Time{}, true},
	}

	for _, tt := range tests {
		got, err := coerceTimestamp(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("coerceTimestamp(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && !got.Equal(tt.want) {
			t.Errorf("coerceTimestamp(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestCoerce_NilHandling(t *testing.T) {
	types := []LogicalType{TypeString, TypeInt64, TypeFloat64, TypeBool, TypeTimestamp}
	for _, lt := range types {
		got, err := coerce(nil, lt)
		if err != nil {
			t.Errorf("coerce(nil, %v) error = %v", lt, err)
		}
		if got != nil {
			t.Errorf("coerce(nil, %v) = %v, want nil", lt, got)
		}
	}
}
