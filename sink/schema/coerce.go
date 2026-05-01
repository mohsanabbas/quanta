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
		return coerceString(val), nil
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

func coerceString(val any) string {
	switch v := val.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
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
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			return t, nil
		}
		t, err = time.Parse(time.RFC3339Nano, v)
		if err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("cannot parse timestamp %q", v)
	case float64:
		return time.Unix(int64(v), 0), nil
	default:
		return time.Time{}, fmt.Errorf("cannot convert %T to timestamp", val)
	}
}
