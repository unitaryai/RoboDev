package llm

import (
	"context"
	"sync"
	"time"
)

// RateLimitedClient wraps a Client and enforces a minimum gap between
// consecutive requests to avoid overwhelming the upstream API.
type RateLimitedClient struct {
	inner   Client
	mu      sync.Mutex
	lastReq time.Time
	minGap  time.Duration
}

// NewRateLimitedClient creates a RateLimitedClient that allows at most rps
// requests per second. If rps <= 0 or inner is nil, no rate limiting is applied.
func NewRateLimitedClient(inner Client, rps float64) *RateLimitedClient {
	var minGap time.Duration
	if rps > 0 && inner != nil {
		minGap = time.Duration(float64(time.Second) / rps)
	}
	return &RateLimitedClient{
		inner:  inner,
		minGap: minGap,
	}
}

// Complete enforces the rate limit, sleeping until minGap has elapsed since
// the last request, then forwards to the inner client.
func (r *RateLimitedClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	r.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(r.lastReq)
	if r.minGap > 0 && elapsed < r.minGap {
		sleep := r.minGap - elapsed
		r.mu.Unlock()
		time.Sleep(sleep)
		r.mu.Lock()
		r.lastReq = time.Now()
	} else {
		r.lastReq = now
	}
	r.mu.Unlock()

	return r.inner.Complete(ctx, req)
}
