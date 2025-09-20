package kafka

import "fmt"

type Registration struct {
	Name string
	New  func() Adapter
}

var registry = map[string]Registration{}

func Register(r Registration) {
	if r.Name == "" {
		panic("source/kafka: registration missing name")
	}
	if r.New == nil {
		panic(fmt.Sprintf("source/kafka: registration %q missing constructor", r.Name))
	}
	registry[r.Name] = r
}

func Lookup(name string) (Registration, bool) { reg, ok := registry[name]; return reg, ok }

func NewAdapter(name string) (Adapter, error) {
	reg, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("kafka: unsupported driver %q", name)
	}
	return reg.New(), nil
}
