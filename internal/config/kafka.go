package config

import (
	kcfg "quanta/source/kafka"
)

func LoadKafkaConfig(path string) (kcfg.Config, error) {
	return kcfg.LoadConfig(path)
}
