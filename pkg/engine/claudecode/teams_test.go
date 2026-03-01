package claudecode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAgentFlags(t *testing.T) {
	tests := []struct {
		name     string
		cfg      TeamsConfig
		taskType string
		wantNil  bool
		check    func(t *testing.T, flags []string)
	}{
		{
			name: "disabled teams returns nil",
			cfg: TeamsConfig{
				Enabled: false,
			},
			taskType: "bug_fix",
			wantNil:  true,
		},
		{
			name: "enabled with no agents and unknown task type returns nil",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 3,
			},
			taskType: "unknown",
			wantNil:  true,
		},
		{
			name: "bug_fix generates coder and reviewer agents",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 3,
			},
			taskType: "bug_fix",
			check: func(t *testing.T, flags []string) {
				require.Len(t, flags, 2)
				assert.Equal(t, "--agents", flags[0])

				var entries []agentFlagEntry
				require.NoError(t, json.Unmarshal([]byte(flags[1]), &entries))
				require.Len(t, entries, 2)

				// Sorted alphabetically: coder, reviewer.
				assert.Equal(t, "coder", entries[0].Name)
				assert.Equal(t, "Write code to fix the issue", entries[0].Role)
				assert.Equal(t, "opus", entries[0].Model)

				assert.Equal(t, "reviewer", entries[1].Name)
				assert.Equal(t, "Review code changes for correctness", entries[1].Role)
				assert.Equal(t, "haiku", entries[1].Model)
			},
		},
		{
			name: "feature generates coder reviewer and tester agents",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 5,
			},
			taskType: "feature",
			check: func(t *testing.T, flags []string) {
				require.Len(t, flags, 2)
				assert.Equal(t, "--agents", flags[0])

				var entries []agentFlagEntry
				require.NoError(t, json.Unmarshal([]byte(flags[1]), &entries))
				require.Len(t, entries, 3)

				// Sorted alphabetically: coder, reviewer, tester.
				assert.Equal(t, "coder", entries[0].Name)
				assert.Equal(t, "reviewer", entries[1].Name)
				assert.Equal(t, "tester", entries[2].Name)
				assert.Equal(t, "sonnet", entries[2].Model)
			},
		},
		{
			name: "custom agents from config override defaults",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 5,
				Agents: map[string]AgentDef{
					"architect": {Role: "Design the solution architecture", Model: "opus"},
					"developer": {Role: "Implement the solution", Model: "sonnet"},
				},
			},
			taskType: "bug_fix",
			check: func(t *testing.T, flags []string) {
				require.Len(t, flags, 2)

				var entries []agentFlagEntry
				require.NoError(t, json.Unmarshal([]byte(flags[1]), &entries))
				require.Len(t, entries, 2)

				// Sorted: architect, developer.
				assert.Equal(t, "architect", entries[0].Name)
				assert.Equal(t, "Design the solution architecture", entries[0].Role)
				assert.Equal(t, "developer", entries[1].Name)
				assert.Equal(t, "Implement the solution", entries[1].Role)
			},
		},
		{
			name: "max teammates limits agent count",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 1,
			},
			taskType: "feature",
			check: func(t *testing.T, flags []string) {
				require.Len(t, flags, 2)

				var entries []agentFlagEntry
				require.NoError(t, json.Unmarshal([]byte(flags[1]), &entries))
				// Feature has 3 default agents but MaxTeammates is 1.
				require.Len(t, entries, 1)
			},
		},
		{
			name: "zero max teammates does not limit",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 0,
			},
			taskType: "feature",
			check: func(t *testing.T, flags []string) {
				require.Len(t, flags, 2)

				var entries []agentFlagEntry
				require.NoError(t, json.Unmarshal([]byte(flags[1]), &entries))
				require.Len(t, entries, 3)
			},
		},
		{
			name: "agents flag is valid JSON",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 5,
				Agents: map[string]AgentDef{
					"coder": {Role: "Write code with \"special\" chars & <entities>"},
				},
			},
			taskType: "bug_fix",
			check: func(t *testing.T, flags []string) {
				require.Len(t, flags, 2)
				assert.True(t, json.Valid([]byte(flags[1])), "agents flag must be valid JSON")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags, err := BuildAgentFlags(tt.cfg, tt.taskType)
			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, flags)
				return
			}

			require.NotNil(t, flags)
			if tt.check != nil {
				tt.check(t, flags)
			}
		})
	}
}

func TestTeamsEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TeamsConfig
		wantNil bool
		check   func(t *testing.T, env map[string]string)
	}{
		{
			name: "disabled returns nil",
			cfg: TeamsConfig{
				Enabled: false,
			},
			wantNil: true,
		},
		{
			name: "enabled sets experimental flag",
			cfg: TeamsConfig{
				Enabled: true,
			},
			check: func(t *testing.T, env map[string]string) {
				assert.Equal(t, "1", env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"])
			},
		},
		{
			name: "enabled with max teammates sets both vars",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 5,
			},
			check: func(t *testing.T, env map[string]string) {
				assert.Equal(t, "1", env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"])
				assert.Equal(t, "5", env["CLAUDE_CODE_MAX_TEAMMATES"])
			},
		},
		{
			name: "zero max teammates omits max teammates var",
			cfg: TeamsConfig{
				Enabled:      true,
				MaxTeammates: 0,
			},
			check: func(t *testing.T, env map[string]string) {
				assert.Equal(t, "1", env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"])
				_, ok := env["CLAUDE_CODE_MAX_TEAMMATES"]
				assert.False(t, ok, "CLAUDE_CODE_MAX_TEAMMATES should not be set when zero")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := TeamsEnvVars(tt.cfg)

			if tt.wantNil {
				assert.Nil(t, env)
				return
			}

			require.NotNil(t, env)
			if tt.check != nil {
				tt.check(t, env)
			}
		})
	}
}

func TestDefaultTeamsConfig(t *testing.T) {
	cfg := DefaultTeamsConfig()

	assert.False(t, cfg.Enabled)
	assert.Equal(t, "in-process", cfg.Mode)
	assert.Equal(t, 3, cfg.MaxTeammates)
	assert.Nil(t, cfg.Agents)
}

func TestDefaultAgentsForTaskType(t *testing.T) {
	tests := []struct {
		name       string
		taskType   string
		wantAgents []string
		wantNil    bool
	}{
		{
			name:       "bug_fix returns coder and reviewer",
			taskType:   "bug_fix",
			wantAgents: []string{"coder", "reviewer"},
		},
		{
			name:       "feature returns coder reviewer and tester",
			taskType:   "feature",
			wantAgents: []string{"coder", "reviewer", "tester"},
		},
		{
			name:     "unknown returns nil",
			taskType: "unknown",
			wantNil:  true,
		},
		{
			name:     "empty returns nil",
			taskType: "",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := defaultAgentsForTaskType(tt.taskType)

			if tt.wantNil {
				assert.Nil(t, agents)
				return
			}

			require.NotNil(t, agents)
			assert.Len(t, agents, len(tt.wantAgents))
			for _, name := range tt.wantAgents {
				_, ok := agents[name]
				assert.True(t, ok, "expected agent %q to be present", name)
			}
		})
	}
}
