package sink

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// DecodeYAML decodes either an inline YAML map (any) or a string path into out.
func DecodeYAML(in any, out any) error {
	if in == nil {
		return fmt.Errorf("missing config")
	}
	switch v := in.(type) {
	case string:
		b, err := os.ReadFile(v)
		if err != nil {
			return fmt.Errorf("read %q: %w", v, err)
		}
		if err := yaml.Unmarshal(b, out); err != nil {
			return fmt.Errorf("parse %q: %w", v, err)
		}
		return nil
	case *yaml.Node:
		if v == nil {
			return fmt.Errorf("missing config")
		}
		raw, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		return yaml.Unmarshal(raw, out)
	default:
		raw, err := yaml.Marshal(in)
		if err != nil {
			return err
		}
		return yaml.Unmarshal(raw, out)
	}
}
