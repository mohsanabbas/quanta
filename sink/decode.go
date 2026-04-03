package sink

import (
	"errors"
	"os"

	qerr "quanta/internal/errors"

	"gopkg.in/yaml.v3"
)

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
