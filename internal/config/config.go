// Package config loads and validates RoboDev controller configuration
// from a YAML file (robodev-config.yaml).
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for the RoboDev controller,
// loaded from robodev-config.yaml.
type Config struct {
	Ticketing     TicketingConfig     `yaml:"ticketing"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Secrets       SecretsConfig       `yaml:"secrets"`
	Engines       EnginesConfig       `yaml:"engines"`
	GuardRails    GuardRailsConfig    `yaml:"guardrails"`
	PluginHealth  PluginHealthConfig  `yaml:"plugin_health"`
}

// TicketingConfig configures the ticketing backend.
type TicketingConfig struct {
	Backend string         `yaml:"backend"`
	Config  map[string]any `yaml:"config"`
}

// NotificationsConfig configures notification channels.
type NotificationsConfig struct {
	Channels []ChannelConfig `yaml:"channels"`
}

// ChannelConfig configures a single notification channel.
type ChannelConfig struct {
	Backend string         `yaml:"backend"`
	Config  map[string]any `yaml:"config"`
}

// SecretsConfig configures the secrets backend.
type SecretsConfig struct {
	Backend string         `yaml:"backend"`
	Config  map[string]any `yaml:"config"`
}

// EnginesConfig configures available execution engines.
type EnginesConfig struct {
	Default string `yaml:"default"`
}

// GuardRailsConfig configures controller-level safety boundaries.
type GuardRailsConfig struct {
	MaxCostPerJob                float64  `yaml:"max_cost_per_job"`
	MaxConcurrentJobs            int      `yaml:"max_concurrent_jobs"`
	MaxJobDurationMinutes        int      `yaml:"max_job_duration_minutes"`
	AllowedRepos                 []string `yaml:"allowed_repos"`
	BlockedFilePatterns          []string `yaml:"blocked_file_patterns"`
	RequireHumanApprovalBeforeMR bool     `yaml:"require_human_approval_before_mr"`
	AllowedTaskTypes             []string `yaml:"allowed_task_types"`
}

// PluginHealthConfig configures plugin health monitoring.
type PluginHealthConfig struct {
	MaxPluginRestarts int      `yaml:"max_plugin_restarts"`
	RestartBackoff    []int    `yaml:"restart_backoff"`
	CriticalPlugins   []string `yaml:"critical_plugins"`
}

// Load reads and parses a RoboDev configuration file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}
