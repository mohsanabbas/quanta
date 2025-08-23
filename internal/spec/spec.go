package spec

type sinkConfigs struct {
	Kafka  any `yaml:"kafka"`
	Stdout any `yaml:"stdout"`
}

type debugSection struct {
	PerFrameDelayMS int  `yaml:"per_frame_delay_ms"`
	PrintCounter    bool `yaml:"print_counter"`
	AckBatchSize    int  `yaml:"ack_batch_size"`
	AckFlushMS      int  `yaml:"ack_flush_ms"`
	PrintValue      bool `yaml:"print_value"`
	ValueMaxBytes   int  `yaml:"value_max_bytes"`
}

type TransformerSpec struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`    // "inproc", "grpc", "stdio", etc.
	Address     string `yaml:"address"` // e.g. "localhost:50051"
	MaxInFlight int    `yaml:"max_in_flight"`
	TimeoutMS   int    `yaml:"timeout_ms"`
	ContentType string `yaml:"content_type"`
	RetryPolicy struct {
		Attempts  int `yaml:"attempts"`
		BackoffMS int `yaml:"backoff_ms"`
	} `yaml:"retry_policy"`
}

type File struct {
	SchemaVersion string `yaml:"schema_version"`

	Source struct {
		Kind   string `yaml:"kind"`
		Driver string `yaml:"driver"`
		Config string `yaml:"config"`
	} `yaml:"source"`

	// Ordered list of transform plugins applied between source and sinks.
	Transformers []TransformerSpec `yaml:"transformers"`

	Sinks       []string     `yaml:"sinks"`
	SinkConfigs sinkConfigs  `yaml:"sink_configs"`
	Debug       debugSection `yaml:"debug"`
}
