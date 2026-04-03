package stdout

import "quanta/sink"

func init() {
	sink.Register(sink.Registration{
		Name:        "stdout",
		New:         func() sink.Adapter { return &driver{} },
		ConfigProto: func() any { return &Config{} },
	})
}
