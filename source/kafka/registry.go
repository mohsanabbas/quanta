package kafka

import "fmt"

// Factory builds an Adapter (e.g., SaramaDriver, KgoDriver, …).
type Factory func() Adapter

var registry = map[string]Factory{}

// Register is called from each driver’s init() or main() factory map.
func Register(name string, f Factory) {
	registry[name] = f
}

// NewAdapter returns a driver by name (“sarama”, “kgo”, “confluent”…).
func NewAdapter(name string) (Adapter, error) {
	if f, ok := registry[name]; ok {
		return f(), nil
	}
	return nil, fmt.Errorf("kafka: unsupported driver %q", name)
}
