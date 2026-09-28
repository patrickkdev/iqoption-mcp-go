package iqoption

import (
	"strings"
	"testing"
	"time"
)

func TestMCPErrorAsRateLimitError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		message         string
		wantLimit       int
		wantRetryAfter  time.Duration
		wantResetAt     time.Time
		wantErrContains string
	}{
		{
			name:           "valid rate limit message",
			message:        "rate_limited: rate limit exceeded (limit=60, retry_after_ms=60000, reset_at=1790372363455)",
			wantLimit:      60,
			wantRetryAfter: time.Minute,
			wantResetAt:    time.UnixMilli(1790372363455),
		},
		{
			name:           "valid message without prefix",
			message:        "rate limit exceeded (limit=10, retry_after_ms=5000, reset_at=1790372363455)",
			wantLimit:      10,
			wantRetryAfter: 5 * time.Second,
			wantResetAt:    time.UnixMilli(1790372363455),
		},
		{
			name:            "missing rate limit data",
			message:         "rate_limited: rate limit exceeded",
			wantErrContains: "invalid rate limit",
		},
		{
			name:            "invalid message",
			message:         "some completely unrelated error",
			wantErrContains: "invalid rate limit message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &MCPError{
				Code:    0,
				Message: tt.message,
				Data:    []byte("null"),
			}

			got, rateErr := err.AsRateLimitError()

			if tt.wantErrContains != "" {
				if rateErr == nil {
					t.Fatalf(
						"expected error containing %q, got nil",
						tt.wantErrContains,
					)
				}

				if !strings.Contains(rateErr.Error(), tt.wantErrContains) {
					t.Fatalf(
						"expected error containing %q, got %q",
						tt.wantErrContains,
						rateErr,
					)
				}

				return
			}

			if rateErr != nil {
				t.Fatalf("unexpected error: %v", rateErr)
			}

			if got == nil {
				t.Fatal("expected rate limit error, got nil")
			}

			if got.Limit != tt.wantLimit {
				t.Errorf(
					"Limit = %d, want %d",
					got.Limit,
					tt.wantLimit,
				)
			}

			if got.RetryAfter != tt.wantRetryAfter {
				t.Errorf(
					"RetryAfter = %s, want %s",
					got.RetryAfter,
					tt.wantRetryAfter,
				)
			}

			if !got.ResetAt.Equal(tt.wantResetAt) {
				t.Errorf(
					"ResetAt = %s, want %s",
					got.ResetAt,
					tt.wantResetAt,
				)
			}

			if got.Message != tt.message {
				t.Errorf(
					"Message = %q, want %q",
					got.Message,
					tt.message,
				)
			}
		})
	}
}

func TestMCPErrorAsRateLimitError_DataIsNull(t *testing.T) {
	t.Parallel()

	err := &MCPError{
		Code:    0,
		Message: "rate_limited: rate limit exceeded (limit=60, retry_after_ms=60000, reset_at=1790372363455)",
		Data:    []byte("null"),
	}

	got, rateErr := err.AsRateLimitError()
	if rateErr != nil {
		t.Fatalf("unexpected error: %v", rateErr)
	}

	if got == nil {
		t.Fatal("expected rate limit error, got nil")
	}

	if got.Limit != 60 {
		t.Errorf("Limit = %d, want 60", got.Limit)
	}

	if got.RetryAfter != time.Minute {
		t.Errorf(
			"RetryAfter = %s, want %s",
			got.RetryAfter,
			time.Minute,
		)
	}

	wantResetAt := time.UnixMilli(1790372363455)

	if !got.ResetAt.Equal(wantResetAt) {
		t.Errorf(
			"ResetAt = %s, want %s",
			got.ResetAt,
			wantResetAt,
		)
	}
}

func TestMCPErrorAsRateLimitError_InvalidMessage(t *testing.T) {
	t.Parallel()

	err := &MCPError{
		Code:    0,
		Message: "rate_limited: rate limit exceeded",
		Data:    []byte("null"),
	}

	_, gotErr := err.AsRateLimitError()
	if gotErr == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(gotErr.Error(), "invalid rate limit message") {
		t.Fatalf(
			"error = %q, want error containing %q",
			gotErr,
			"invalid rate limit message",
		)
	}
}
