// Package config handles application configuration via viper.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config holds the runtime configuration for g9s.
type Config struct {
	Project  string `mapstructure:"project"`
	LogLevel string `mapstructure:"log_level"`
}

// Load reads config from viper and returns a Config struct.
// If no project is set via flag or env var, it falls back to the active
// gcloud configuration (~/.config/gcloud/configurations/config_<active>).
func Load() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	if cfg.Project == "" {
		cfg.Project = gcloudActiveProject()
	}
	return &cfg, nil
}

// gcloudActiveProject reads the project from the active gcloud configuration.
// Returns an empty string if the file cannot be read or has no project set.
func gcloudActiveProject() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// Resolve the active configuration name.
	activeFile := filepath.Join(home, ".config", "gcloud", "active_config")
	nameBytes, err := os.ReadFile(activeFile)
	if err != nil {
		return ""
	}
	configName := strings.TrimSpace(string(nameBytes))
	if configName == "" {
		return ""
	}

	// Parse the INI-style properties file for the [core] project key.
	propsFile := filepath.Join(home, ".config", "gcloud", "configurations", "config_"+configName)
	f, err := os.Open(propsFile)
	if err != nil {
		return ""
	}
	defer f.Close()

	inCore := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[core]" {
			inCore = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inCore = false
			continue
		}
		if inCore {
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "project" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}
