package llm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTimingClient records the wall-clock time of each Complete call.
type mockTimingClient struct {
	mu        sync.Mutex
	callTimes []time.Time
}

func (m *mockTimingClient) Complete(_ context.Context, _ CompletionRequest) (*CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callTimes = append(m.callTimes, time.Now())
	return &CompletionResponse{Content: "ok"}, nil
}

func TestRateLimitedClient_EnforcesMinGap(t *testing.T) {
	mock := &mockTimingClient{}
	// rps=2 → minGap = 500ms
	rl := NewRateLimitedClient(mock, 2)

	ctx := context.Background()

	_, err := rl.Complete(ctx, CompletionRequest{})
	require.NoError(t, err)

	_, err = rl.Complete(ctx, CompletionRequest{})
	require.NoError(t, err)

	require.Len(t, mock.callTimes, 2)

	gap := mock.callTimes[1].Sub(mock.callTimes[0])
	// Allow 20% slack: 500ms * 0.80 = 400ms.
	assert.GreaterOrEqual(t, gap.Milliseconds(), int64(400),
		"expected at least 400ms gap between calls, got %v", gap)
}

func TestRateLimitedClient_NoLimitWhenZeroRPS(t *testing.T) {
	mock := &mockTimingClient{}
	rl := NewRateLimitedClient(mock, 0)

	ctx := context.Background()

	_, err := rl.Complete(ctx, CompletionRequest{})
	require.NoError(t, err)

	_, err = rl.Complete(ctx, CompletionRequest{})
	require.NoError(t, err)

	require.Len(t, mock.callTimes, 2)

	gap := mock.callTimes[1].Sub(mock.callTimes[0])
	// With no rate limit, calls should complete in under 100ms.
	assert.Less(t, gap.Milliseconds(), int64(100),
		"expected no significant delay between calls, got %v", gap)
}
