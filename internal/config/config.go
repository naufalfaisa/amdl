// ============================================
// File: internal/config/config.go
package config

import (
	"main/utils/structs"
	"os"

	"gopkg.in/yaml.v2"
)

type Config = structs.ConfigSet

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	if len(cfg.Storefront) != 2 {
		cfg.Storefront = "us"
	}

	return &cfg, nil
}

func LimitString(s string, maxLen int) string {
	if len([]rune(s)) > maxLen {
		return string([]rune(s)[:maxLen])
	}
	return s
}
