package config

import (
	"main/internal/structs"
	"os"

	"gopkg.in/yaml.v2"
)

// LoadConfig reads the config.yaml file and returns the configuration.
func LoadConfig() (*structs.ConfigSet, error) {
	var cfg structs.ConfigSet
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	if len(cfg.Storefront) != 2 {
		cfg.Storefront = "us"
	}
	return &cfg, nil
}
