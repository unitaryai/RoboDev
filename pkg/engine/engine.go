// Package engine defines the ExecutionEngine interface and associated types
// used to describe units of work to be performed by AI coding agents.
// Engines are responsible for translating tasks into engine-agnostic
// ExecutionSpecs, which the core JobBuilder then translates into
// Kubernetes Jobs or other runtime constructs.
package engine

// TokenUsage tracks token consumption for cost accounting.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// TaskResult is the structured result written by the engine to /workspace/result.json.
type TaskResult struct {
	Success         bool        `json:"success"`
	MergeRequestURL string      `json:"merge_request_url,omitempty"`
	BranchName      string      `json:"branch_name,omitempty"`
	Summary         string      `json:"summary"`
	TokenUsage      *TokenUsage `json:"token_usage,omitempty"`
	CostEstimateUSD float64     `json:"cost_estimate_usd,omitempty"`
	ExitCode        int         `json:"exit_code"` // 0=success, 1=agent failure, 2=guard rail blocked
}

// Resources describes CPU and memory requirements.
type Resources struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// VolumeMount describes a volume to mount into the execution container.
type VolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mount_path"`
	ReadOnly  bool   `json:"read_only,omitempty"`
}

// Task represents a unit of work to be performed by an engine.
type Task struct {
	ID          string            `json:"id"`
	TicketID    string            `json:"ticket_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	RepoURL     string            `json:"repo_url"`
	Labels      []string          `json:"labels,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// EngineConfig holds engine-specific configuration.
type EngineConfig struct {
	Image            string            `json:"image"`
	ResourceRequests Resources         `json:"resource_requests"`
	ResourceLimits   Resources         `json:"resource_limits"`
	TimeoutSeconds   int               `json:"timeout_seconds"`
	Env              map[string]string `json:"env,omitempty"`
}

// ExecutionSpec is an engine-agnostic description of what to run.
// The core JobBuilder translates this into a K8s Job (or Docker run, etc).
type ExecutionSpec struct {
	Image                 string            `json:"image"`
	Command               []string          `json:"command"`
	Env                   map[string]string `json:"env"`
	SecretEnv             map[string]string `json:"secret_env"`
	ResourceRequests      Resources         `json:"resource_requests"`
	ResourceLimits        Resources         `json:"resource_limits"`
	Volumes               []VolumeMount     `json:"volumes"`
	ActiveDeadlineSeconds int               `json:"active_deadline_seconds"`
}

// ExecutionEngine wraps an AI coding tool (Claude Code, Codex, etc).
type ExecutionEngine interface {
	// BuildExecutionSpec returns a runtime-agnostic spec; the core JobBuilder
	// handles translation to K8s Jobs, Docker containers, etc.
	BuildExecutionSpec(task Task, config EngineConfig) (*ExecutionSpec, error)

	// BuildPrompt constructs the task prompt for this engine.
	BuildPrompt(task Task) (string, error)

	// Name returns a unique engine identifier (e.g. "claude-code", "codex").
	Name() string

	// InterfaceVersion returns the version this engine implements.
	InterfaceVersion() int
}
