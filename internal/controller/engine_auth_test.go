package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"

	"github.com/unitaryai/osmia/internal/config"
	"github.com/unitaryai/osmia/pkg/engine"
	"github.com/unitaryai/osmia/pkg/engine/claudecode"
)

// hintedMockEngine is a test double implementing both engine.ExecutionEngine
// and engine.CredentialHints, used to verify that baseEngineConfig's auth
// merge generalises to any engine declaring credential hints, not just
// claude-code.
type hintedMockEngine struct {
	mockEngine
	envName    string
	candidates []string
}

func (m *hintedMockEngine) APIKeyEnvName() string         { return m.envName }
func (m *hintedMockEngine) APIKeyKeyCandidates() []string { return m.candidates }

var _ engine.CredentialHints = (*hintedMockEngine)(nil)

// TestBaseEngineConfig_ClaudeCodeProbeOrderUnchanged pins the exact probe
// order for claude-code (ANTHROPIC_API_KEY before api_key) that existed
// before the auth merge was generalised via AuthFor/CredentialHints. It uses
// the real claudecode.ClaudeCodeEngine so that the CredentialHints values
// exercised are the ones actually shipped, not a stand-in.
func TestBaseEngineConfig_ClaudeCodeProbeOrderUnchanged(t *testing.T) {
	tests := []struct {
		name       string
		secretData map[string][]byte
		wantKey    string
	}{
		{
			name: "both candidate keys present — ANTHROPIC_API_KEY wins",
			secretData: map[string][]byte{
				"ANTHROPIC_API_KEY": []byte("sk-ant-1"),
				"api_key":           []byte("sk-legacy"),
			},
			wantKey: "ANTHROPIC_API_KEY",
		},
		{
			name: "only legacy api_key present",
			secretData: map[string][]byte{
				"api_key": []byte("sk-legacy"),
			},
			wantKey: "api_key",
		},
		{
			name:       "neither candidate present",
			secretData: map[string][]byte{"unrelated": []byte("x")},
			wantKey:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := fake.NewSimpleClientset(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "anthropic-secret", Namespace: "test-ns"},
				Data:       tt.secretData,
			})

			cfg := &config.Config{
				Engines: config.EnginesConfig{
					ClaudeCode: &config.ClaudeCodeEngineConfig{
						Auth: config.AuthConfig{APIKeySecret: "anthropic-secret"},
					},
				},
			}

			r := NewReconciler(cfg, testLogger(),
				WithEngine(claudecode.New()),
				WithK8sClient(k8s),
				WithNamespace("test-ns"),
			)

			got := r.baseEngineConfig(context.Background(), "claude-code")
			assert.Equal(t, "anthropic-secret", got.APIKeySecret)
			assert.Equal(t, tt.wantKey, got.APIKeyKey)
		})
	}
}

// TestBaseEngineConfig_ClaudeCodeExplicitKeySkipsProbe verifies that an
// explicit api_key_key bypasses probing entirely, as before.
func TestBaseEngineConfig_ClaudeCodeExplicitKeySkipsProbe(t *testing.T) {
	k8s := fake.NewSimpleClientset()
	cfg := &config.Config{
		Engines: config.EnginesConfig{
			ClaudeCode: &config.ClaudeCodeEngineConfig{
				Auth: config.AuthConfig{APIKeySecret: "anthropic-secret", APIKeyKey: "custom-key"},
			},
		},
	}

	r := NewReconciler(cfg, testLogger(),
		WithEngine(claudecode.New()),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	got := r.baseEngineConfig(context.Background(), "claude-code")
	assert.Equal(t, "anthropic-secret", got.APIKeySecret)
	assert.Equal(t, "custom-key", got.APIKeyKey)
}

// TestBaseEngineConfig_ClaudeCodeFallsBackWithoutRegisteredEngine verifies
// that claude-code still gets the historical probe order even when no
// claude-code engine instance is registered under r.engines (e.g. a minimal
// test setup), preserving byte-identical behaviour for the special-cased
// engine name.
func TestBaseEngineConfig_ClaudeCodeFallsBackWithoutRegisteredEngine(t *testing.T) {
	k8s := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "anthropic-secret", Namespace: "test-ns"},
		Data: map[string][]byte{
			"ANTHROPIC_API_KEY": []byte("sk-ant-1"),
			"api_key":           []byte("sk-legacy"),
		},
	})
	cfg := &config.Config{
		Engines: config.EnginesConfig{
			ClaudeCode: &config.ClaudeCodeEngineConfig{
				Auth: config.AuthConfig{APIKeySecret: "anthropic-secret"},
			},
		},
	}

	r := NewReconciler(cfg, testLogger(),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	got := r.baseEngineConfig(context.Background(), "claude-code")
	assert.Equal(t, "ANTHROPIC_API_KEY", got.APIKeyKey)
}

// TestBaseEngineConfig_UnhintedEngineUnchanged verifies that engines which do
// not implement engine.CredentialHints (every built-in engine other than
// claude-code, at present) keep their pre-generalisation behaviour: the auth
// merge is skipped entirely and APIKeySecret/APIKeyKey stay empty, even when
// the operator has configured an Auth block for that engine.
func TestBaseEngineConfig_UnhintedEngineUnchanged(t *testing.T) {
	k8s := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "codex-secret", Namespace: "test-ns"},
		Data:       map[string][]byte{"OPENAI_API_KEY": []byte("sk-openai")},
	})
	cfg := &config.Config{
		Engines: config.EnginesConfig{
			Codex: &config.CodexEngineConfig{
				Auth: config.AuthConfig{APIKeySecret: "codex-secret"},
			},
		},
	}

	r := NewReconciler(cfg, testLogger(),
		WithEngine(&mockEngine{name: "codex"}),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	got := r.baseEngineConfig(context.Background(), "codex")
	assert.Empty(t, got.APIKeySecret)
	assert.Empty(t, got.APIKeyKey)
}

// TestBaseEngineConfig_GeneralisesToAnyCredentialHintsEngine verifies that
// the auth merge applies to any registered engine implementing
// engine.CredentialHints, using its declared probe order, not just
// claude-code. This is the behaviour PR 1.2 introduces.
func TestBaseEngineConfig_GeneralisesToAnyCredentialHintsEngine(t *testing.T) {
	k8s := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "codex-secret", Namespace: "test-ns"},
		Data: map[string][]byte{
			"OPENAI_API_KEY": []byte("sk-openai"),
			"api_key":        []byte("sk-legacy"),
		},
	})
	cfg := &config.Config{
		Engines: config.EnginesConfig{
			Codex: &config.CodexEngineConfig{
				Auth: config.AuthConfig{APIKeySecret: "codex-secret"},
			},
		},
	}

	hinted := &hintedMockEngine{
		mockEngine: mockEngine{name: "codex"},
		envName:    "OPENAI_API_KEY",
		candidates: []string{"OPENAI_API_KEY", "api_key"},
	}

	r := NewReconciler(cfg, testLogger(),
		WithEngine(hinted),
		WithK8sClient(k8s),
		WithNamespace("test-ns"),
	)

	got := r.baseEngineConfig(context.Background(), "codex")
	assert.Equal(t, "codex-secret", got.APIKeySecret)
	assert.Equal(t, "OPENAI_API_KEY", got.APIKeyKey)
}
