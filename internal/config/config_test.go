package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    *Config
		wantErr bool
	}{
		{
			name: "valid minimal config",
			yaml: `
ticketing:
  backend: github
secrets:
  backend: env
engines:
  default: claude-code
guardrails:
  max_cost_per_job: 5.0
  max_concurrent_jobs: 10
  max_job_duration_minutes: 60
`,
			want: &Config{
				Ticketing: TicketingConfig{Backend: "github"},
				Secrets:   SecretsConfig{Backend: "env"},
				Engines:   EnginesConfig{Default: "claude-code"},
				GuardRails: GuardRailsConfig{
					MaxCostPerJob:         5.0,
					MaxConcurrentJobs:     10,
					MaxJobDurationMinutes: 60,
				},
			},
		},
		{
			name: "config with notifications and plugin health",
			yaml: `
ticketing:
  backend: jira
  config:
    url: https://example.atlassian.net
notifications:
  channels:
    - backend: slack
      config:
        webhook_url: https://hooks.slack.com/example
secrets:
  backend: aws-secrets-manager
engines:
  default: codex
guardrails:
  max_cost_per_job: 10.0
  max_concurrent_jobs: 5
  max_job_duration_minutes: 120
  allowed_repos:
    - github.com/example/repo
  blocked_file_patterns:
    - "*.env"
    - "secrets/**"
  require_human_approval_before_mr: true
  allowed_task_types:
    - dependency-update
    - bug-fix
plugin_health:
  max_plugin_restarts: 3
  restart_backoff:
    - 1
    - 5
    - 30
  critical_plugins:
    - ticketing
    - notifications
`,
			want: &Config{
				Ticketing: TicketingConfig{
					Backend: "jira",
					Config:  map[string]any{"url": "https://example.atlassian.net"},
				},
				Notifications: NotificationsConfig{
					Channels: []ChannelConfig{
						{
							Backend: "slack",
							Config:  map[string]any{"webhook_url": "https://hooks.slack.com/example"},
						},
					},
				},
				Secrets: SecretsConfig{Backend: "aws-secrets-manager"},
				Engines: EnginesConfig{Default: "codex"},
				GuardRails: GuardRailsConfig{
					MaxCostPerJob:                10.0,
					MaxConcurrentJobs:            5,
					MaxJobDurationMinutes:        120,
					AllowedRepos:                 []string{"github.com/example/repo"},
					BlockedFilePatterns:          []string{"*.env", "secrets/**"},
					RequireHumanApprovalBeforeMR: true,
					AllowedTaskTypes:             []string{"dependency-update", "bug-fix"},
				},
				PluginHealth: PluginHealthConfig{
					MaxPluginRestarts: 3,
					RestartBackoff:    []int{1, 5, 30},
					CriticalPlugins:   []string{"ticketing", "notifications"},
				},
			},
		},
		{
			name:    "invalid yaml",
			yaml:    ":\tinvalid: yaml: content",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write the YAML to a temporary file.
			tmp := filepath.Join(t.TempDir(), "robodev-config.yaml")
			err := os.WriteFile(tmp, []byte(tt.yaml), 0o600)
			require.NoError(t, err)

			got, err := Load(tmp)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/robodev-config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}
