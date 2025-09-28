package kafka

import "quanta/sink"

func Registration() sink.Registration {
	return sink.Registration{
		Name:        "kafka",
		New:         func() sink.Adapter { return &SaramaSink{} },
		ConfigProto: func() any { return &Config{} },
	}
}
