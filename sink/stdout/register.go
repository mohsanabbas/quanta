package stdout

import "quanta/sink"

// Registration exposes stdout sink without using init().
func Registration() sink.Registration {
	return sink.Registration{
		Name: "stdout",
		New:  func() sink.Adapter { return &driver{} },
		// ConfigProto returns a *pointer* so yaml unmarshalling works directly.
		ConfigProto: func() any { return &Config{} },
	}
}
