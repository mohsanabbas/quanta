package kafka

import "fmt"

type Factory func() Adapter

var registry = map[string]Factory{}

func Register(name string, f Factory) {
	registry[name] = f
}

func NewAdapter(name string) (Adapter, error) {
	if f, ok := registry[name]; ok {
		return f(), nil
	}
	return nil, fmt.Errorf("kafka: unsupported driver %q", name)
}
