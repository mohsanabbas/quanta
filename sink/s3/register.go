package s3

import "quanta/sink"

func init() {
	sink.Register(sink.Registration{
		Name:        "s3",
		New:         func() sink.Adapter { return &Driver{} },
		ConfigProto: func() any { return &Config{} },
	})
}
