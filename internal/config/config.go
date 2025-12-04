// ============================================
// File: internal/config/config.go
package config

import (
	"fmt"
	"os"

	"github.com/naufalfaisa/amdl/pkg/structs"

	"gopkg.in/yaml.v2"
)

// Config is an alias for the main configuration structure
type Config = structs.ConfigSet

// Constants
const (
	defaultStorefront = "us"
	storefrontCodeLen = 2
)

// Load reads and parses configuration from a YAML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}

	// Set default storefront if invalid
	if len(cfg.Storefront) != storefrontCodeLen {
		cfg.Storefront = defaultStorefront
	}

	return &cfg, nil
}

// LimitString truncates a string to maxLen characters (rune-aware for Unicode)
func LimitString(s string, maxLen int) string {
	if maxLen < 0 {
		return s
	}

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	return string(runes[:maxLen])
}
