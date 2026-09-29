package iqoption

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestNewRateLimiter_Defaults(t *testing.T) {
	t.Parallel()

	l := NewRateLimiter()

	if got := l.Gateway.Limit(); got != NewRateLimit(200) {
		t.Errorf("Gateway limit = %v, want %v", got, NewRateLimit(200))
	}
	if got := l.Read.Limit(); got != NewRateLimit(60) {
		t.Errorf("Read limit = %v, want %v", got, NewRateLimit(60))
	}
	if got := l.Write.Limit(); got != NewRateLimit(10) {
		t.Errorf("Write limit = %v, want %v", got, NewRateLimit(10))
	}

	if got := l.Gateway.Burst(); got != 1 {
		t.Errorf("Gateway burst = %d, want 1", got)
	}
	if got := l.Read.Burst(); got != 1 {
		t.Errorf("Read burst = %d, want 1", got)
	}
	if got := l.Write.Burst(); got != 1 {
		t.Errorf("Write burst = %d, want 1", got)
	}
}

func TestNewRateLimiterFromLimits(t *testing.T) {
	t.Parallel()

	limits := RateLimits{
		GatewayPerMinute: 100,
		ReadPerMinute:    50,
		WritePerMinute:   20,
	}

	l := NewRateLimiterFromLimits(limits)

	tests := []struct {
		name string
		got  rate.Limit
		want rate.Limit
	}{
		{"gateway", l.Gateway.Limit(), NewRateLimit(100)},
		{"read", l.Read.Limit(), NewRateLimit(50)},
		{"write", l.Write.Limit(), NewRateLimit(20)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("limit = %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestNewLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		perMinute int
		want      rate.Limit
	}{
		{
			perMinute: 100,
			want:      rate.Every(time.Minute / 90),
		},
		{
			perMinute: 10,
			want:      rate.Every(time.Minute / 9),
		},
		{
			perMinute: 1,
			want:      rate.Every(time.Minute),
		},
		{
			perMinute: 0,
			want:      rate.Every(time.Minute),
		},
		{
			perMinute: -100,
			want:      rate.Every(time.Minute),
		},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.perMinute)), func(t *testing.T) {
			if got := NewRateLimit(tt.perMinute); got != tt.want {
				t.Errorf("NewLimit(%d) = %v, want %v", tt.perMinute, got, tt.want)
			}
		})
	}
}

func TestRateLimiter_Update(t *testing.T) {
	t.Parallel()

	l := NewRateLimiter()

	limits := RateLimits{
		GatewayPerMinute: 100,
		ReadPerMinute:    50,
		WritePerMinute:   20,
	}

	l.Update(limits)

	if got, want := l.Gateway.Limit(), NewRateLimit(100); got != want {
		t.Errorf("Gateway limit = %v, want %v", got, want)
	}
	if got, want := l.Read.Limit(), NewRateLimit(50); got != want {
		t.Errorf("Read limit = %v, want %v", got, want)
	}
	if got, want := l.Write.Limit(), NewRateLimit(20); got != want {
		t.Errorf("Write limit = %v, want %v", got, want)
	}

	if got := l.Gateway.Burst(); got != 1 {
		t.Errorf("Gateway burst = %d, want 1", got)
	}
	if got := l.Read.Burst(); got != 1 {
		t.Errorf("Read burst = %d, want 1", got)
	}
	if got := l.Write.Burst(); got != 1 {
		t.Errorf("Write burst = %d, want 1", got)
	}
}

func TestRateLimiter_WaitRead(t *testing.T) {
	t.Parallel()

	l := NewRateLimiterFromLimits(RateLimits{
		GatewayPerMinute: 1000,
		ReadPerMinute:    1000,
		WritePerMinute:   1000,
	})

	ctx := context.Background()

	if err := l.Read.Wait(ctx); err != nil {
		t.Fatalf("first wait read error = %v", err)
	}

	if err := l.Read.Wait(ctx); err != nil {
		t.Fatalf("second wait read error = %v", err)
	}
}

func TestRateLimiter_WaitWrite(t *testing.T) {
	t.Parallel()

	l := NewRateLimiterFromLimits(RateLimits{
		GatewayPerMinute: 1000,
		ReadPerMinute:    1000,
		WritePerMinute:   1000,
	})

	ctx := context.Background()

	if err := l.Write.Wait(ctx); err != nil {
		t.Fatalf("first wait write error = %v", err)
	}

	if err := l.Write.Wait(ctx); err != nil {
		t.Fatalf("second wait write error = %v", err)
	}
}

func TestRateLimiter_WaitGateway(t *testing.T) {
	t.Parallel()

	l := NewRateLimiterFromLimits(RateLimits{
		GatewayPerMinute: 1000,
		ReadPerMinute:    1000,
		WritePerMinute:   1000,
	})

	ctx := context.Background()

	if err := l.Gateway.Wait(ctx); err != nil {
		t.Fatalf("first wait gateway error = %v", err)
	}

	if err := l.Gateway.Wait(ctx); err != nil {
		t.Fatalf("second wait gateway error = %v", err)
	}
}

func TestRateLimiter_WaitRead_CancelledContext(t *testing.T) {
	t.Parallel()

	l := NewRateLimiterFromLimits(RateLimits{
		GatewayPerMinute: 1,
		ReadPerMinute:    1,
		WritePerMinute:   1,
	})

	// Consume the initial gateway token.
	if err := l.Gateway.Wait(context.Background()); err != nil {
		t.Fatalf("consuming gateway token: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := l.Read.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitRead() error = %v, want %v", err, context.Canceled)
	}
}

func TestRateLimiter_WaitWrite_CancelledContext(t *testing.T) {
	t.Parallel()

	l := NewRateLimiterFromLimits(RateLimits{
		GatewayPerMinute: 1,
		ReadPerMinute:    1,
		WritePerMinute:   1,
	})

	// Consume the initial gateway token.
	if err := l.Gateway.Wait(context.Background()); err != nil {
		t.Fatalf("consuming gateway token: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := l.Write.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitWrite() error = %v, want %v", err, context.Canceled)
	}
}

func TestRateLimiter_WaitGateway_Cancellation(t *testing.T) {
	t.Parallel()

	l := NewRateLimiterFromLimits(RateLimits{
		GatewayPerMinute: 1,
		ReadPerMinute:    1000,
		WritePerMinute:   1000,
	})

	if err := l.Gateway.Wait(context.Background()); err != nil {
		t.Fatalf("consuming gateway token: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := l.Gateway.Wait(ctx)
	if err == nil {
		t.Fatal("WaitWrite() error = nil, want an error")
	}
}
