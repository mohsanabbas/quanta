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

// RegisterDefaults registers all built‑in Kafka source drivers. It should be
// called during application start‑up before attempting to construct a driver
// via NewAdapter(). Without registration NewAdapter() will return an
// unsupported driver error. RegisterDefaults is idempotent.
func RegisterDefaults() {
	// Register the Sarama driver under the name "sarama". Additional drivers
	// (e.g. franz‑go or confluent) can be registered here when implemented.
	Register(Registration{
		Name: "sarama",
		New: func() Adapter {
			return &SaramaDriver{}
		},
	})
	// Placeholder for other drivers; no‑op if not implemented. Users can
	// manually register their own drivers as well.
}
