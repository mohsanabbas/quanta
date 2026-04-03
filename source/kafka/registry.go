package kafka

import (
	"fmt"

	qerr "quanta/internal/errors"
)

type Registration struct {
	Name string
	New  func() Adapter
}

var _registry = map[string]Registration{}

func Register(r Registration) {
	if r.Name == "" {
		panic("source/kafka: registration missing name")
	}
	if r.New == nil {
		panic("source/kafka: registration " + r.Name + " missing constructor")
	}
	_registry[r.Name] = r
}

func Lookup(name string) (Registration, bool) {
	reg, ok := _registry[name]
	return reg, ok
}

func NewAdapter(name string) (Adapter, error) {
	reg, ok := _registry[name]
	if !ok {
		return nil, qerr.Source("kafka", "create", fmt.Errorf("unsupported driver %q", name))
	}
	return reg.New(), nil
}

func RegisterDefaults() {
	Register(Registration{
		Name: "sarama",
		New:  func() Adapter { return &SaramaDriver{} },
	})
}
