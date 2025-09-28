package pipeline

import (
	"quanta/sink"
	sinkKafka "quanta/sink/kafka"
	sinkStdout "quanta/sink/stdout"
)

// registerBuiltins is called once before we compile the pipeline.
func registerBuiltins() {
	sink.Register(sinkStdout.Registration())
	sink.Register(sinkKafka.Registration())
}
