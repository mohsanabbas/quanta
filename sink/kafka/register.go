package kafka

import "quanta/sink"

func init() {
	sink.Register(sink.Registration{
		Name:        "kafka",
		New:         func() sink.Adapter { return &SaramaSink{} },
		ConfigProto: func() any { return &Config{} },
	})
}
