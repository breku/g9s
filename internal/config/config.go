// Package config handles application configuration via viper.
package config

import "github.com/spf13/viper"

// Config holds the runtime configuration for gcptui.
type Config struct {
	Project  string `mapstructure:"project"`
	LogLevel string `mapstructure:"log_level"`
	Region   string `mapstructure:"region"`
}

// Load reads config from viper and returns a Config struct.
func Load() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
