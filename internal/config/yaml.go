package config

import (
	"errors"
	"os"

	qerr "quanta/internal/errors"

	"gopkg.in/yaml.v3"
)

// DecodeYAML normalises a heterogeneous "config" input into a typed Go value.
//
// It accepts:
//   - a string: treated as a path to a YAML file on disk
//   - a *yaml.Node: an already-parsed YAML node (e.g. from a parent document)
//   - any other value: marshalled to YAML and re-unmarshalled into out
//
// The single Component used in errors is empty by convention callers that
// want to attribute the error to a specific driver should wrap with
// qerr.Config(driver, "decode", err) at the call site.
func DecodeYAML(in any, out any) error {
	if in == nil {
		return qerr.Config("", "decode", errors.New("missing config"))
	}
	switch v := in.(type) {
	case string:
		b, err := os.ReadFile(v)
		if err != nil {
			return qerr.Config("", "read", err)
		}
		if err := yaml.Unmarshal(b, out); err != nil {
			return qerr.Config("", "parse", err)
		}
		return nil
	case *yaml.Node:
		if v == nil {
			return qerr.Config("", "decode", errors.New("missing config"))
		}
		raw, err := yaml.Marshal(v)
		if err != nil {
			return qerr.Config("", "marshal", err)
		}
		return yaml.Unmarshal(raw, out)
	default:
		raw, err := yaml.Marshal(v)
		if err != nil {
			return qerr.Config("", "marshal", err)
		}
		return yaml.Unmarshal(raw, out)
	}
}
