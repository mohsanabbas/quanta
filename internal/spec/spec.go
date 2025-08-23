package spec

// optional per-sink config bucket
type sinkConfigs struct {
	Kafka  any `yaml:"kafka"`  // let driver cast
	Stdout any `yaml:"stdout"` // ditto
}

type debugSection struct {
	PerFrameDelayMS int  `yaml:"per_frame_delay_ms"`
	PrintCounter    bool `yaml:"print_counter"`
	AckBatchSize    int  `yaml:"ack_batch_size"`
	AckFlushMS      int  `yaml:"ack_flush_ms"`
}

type File struct {
	Source struct {
		Kind   string `yaml:"kind"`
		Driver string `yaml:"driver"`
		Config string `yaml:"config"`
	} `yaml:"source"`

	Sinks       []string     `yaml:"sinks"`
	SinkConfigs sinkConfigs  `yaml:"sink_configs"`
	Debug       debugSection `yaml:"debug"`
}
