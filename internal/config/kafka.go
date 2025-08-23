package config

import (
	kcfg "quanta/source/kafka"
)

// LoadKafkaConfig delegates to the Kafka source loader while centralizing
// loader entrypoints under internal/config.
func LoadKafkaConfig(path string) (kcfg.Config, error) {
	return kcfg.LoadConfig(path)
}
