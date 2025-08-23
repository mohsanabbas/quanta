// // FILES
// // -----
// // adapter.go          – public Adapter interface + EmitFunc
// // config.go           – unified YAML/JSON config structs & loader
// // backpressure.go     – credit/token‑bucket flow‑control
// // checkpoint.go       – pluggable offset store + Manager
// // registry.go         – global registry of driver factories
// // driver_sarama.go    – implementation based on Shopify/sarama (build tag: sarama)
// // driver_kgo.go       – implementation based on franz-go/kgo   (build tag: kgo)
// // driver_confluent.go – implementation based on confluent-kafka-go (build tag: confluent)
// //
// // Note: Build tags allow you to compile only the driver you need, e.g.:
// //   go build -tags=kgo ./cmd/engine
// // -----------------------------------------------------------------------------

package kafka

import (
	"context"
	pb "quanta/api/proto/v1"
)

type EmitFunc func(*pb.Frame) error

type Adapter interface {
	Configure(Config) error
	Run(context.Context, EmitFunc) error
	Close() error
}
