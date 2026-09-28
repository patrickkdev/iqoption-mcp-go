package iqoption

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type RateLimits struct {
	GatewayPerMinute int
	ReadPerMinute    int
	WritePerMinute   int
}

type RateLimiter struct {
	Gateway *rate.Limiter
	Read    *rate.Limiter
	Write   *rate.Limiter

	mu sync.Mutex
}

func NewRateLimiter() *RateLimiter {
	return NewRateLimiterFromLimits(RateLimits{
		GatewayPerMinute: 200,
		ReadPerMinute:    60,
		WritePerMinute:   10,
	})
}

func NewRateLimiterFromLimits(limits RateLimits) *RateLimiter {
	return &RateLimiter{
		Gateway: rate.NewLimiter(NewRateLimit(limits.GatewayPerMinute), 1),
		Read:    rate.NewLimiter(NewRateLimit(limits.ReadPerMinute), 1),
		Write:   rate.NewLimiter(NewRateLimit(limits.WritePerMinute), 1),
	}
}

func NewRateLimit(perMinute int) rate.Limit {
	// Keep some headroom for server-side fixed-window boundaries,
	// clock skew and requests made outside this limiter.
	safePerMinute := perMinute * 9 / 10
	return rate.Every(time.Minute / time.Duration(max(1, safePerMinute)))
}

func (l *RateLimiter) Update(limits RateLimits) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.Gateway.SetLimit(NewRateLimit(limits.GatewayPerMinute))
	l.Read.SetLimit(NewRateLimit(limits.ReadPerMinute))
	l.Write.SetLimit(NewRateLimit(limits.WritePerMinute))

	l.Gateway.SetBurst(1)
	l.Read.SetBurst(1)
	l.Write.SetBurst(1)
}

// WaitRead reserves both the gateway and read buckets.
//
// We deliberately reserve the gateway first because every request
// consumes it. If waiting for the second bucket is cancelled, the
// gateway reservation may have been consumed; callers should simply
// propagate the context cancellation.
func (l *RateLimiter) WaitRead(ctx context.Context) error {
	if err := l.Gateway.Wait(ctx); err != nil {
		return err
	}

	return l.Read.Wait(ctx)
}

func (l *RateLimiter) WaitWrite(ctx context.Context) error {
	if err := l.Gateway.Wait(ctx); err != nil {
		return err
	}

	return l.Write.Wait(ctx)
}
