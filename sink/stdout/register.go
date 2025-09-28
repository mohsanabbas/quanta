package stdout

import "quanta/sink"

// Registration exposes stdout sink.
func Registration() sink.Registration {
	return sink.Registration{
		Name:        "stdout",
		New:         func() sink.Adapter { return &driver{} },
		ConfigProto: func() any { return &Config{} },
	}
}
