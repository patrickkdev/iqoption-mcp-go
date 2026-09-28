package iqoption

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrClosed is returned when an operation is attempted after Close.
	ErrClosed = errors.New("iqoption: client is closed")
	// ErrNetwork indicates a transport/network failure.
	ErrNetwork = errors.New("iqoption: network error")
	// ErrUnauthorized indicates that the configured token was rejected.
	ErrUnauthorized = errors.New("iqoption: unauthorized")
	// ErrServerUnavailable indicates a server-side 5xx response.
	ErrServerUnavailable = errors.New("iqoption: server unavailable")
	// ErrSessionExpired indicates that the MCP session is no longer valid.
	ErrSessionExpired = errors.New("iqoption: MCP session expired")
	// ErrValidation indicates that the request data was invalid.
	ErrValidation = errors.New("iqoption: validation error")
	// ErrTradingDenied indicates that the user does not have access to trading.
	ErrTradingDenied = errors.New("iqoption: trading access denied")
	// ErrToolNotFound indicates that the requested MCP tool was not found.
	ErrToolNotFound = errors.New("iqoption: MCP tool not found")
	// ErrRateLimited indicates that the server rejected the request because the client exceeded a rate limit.
	ErrRateLimited = errors.New("iqoption: rate limited")
)

// MCPError represents an MCP/JSON-RPC error returned by the server.
type MCPError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *MCPError) Error() string {
	if e.Data == nil {
		return fmt.Sprintf(
			"mcp error %d: %s",
			e.Code,
			e.Message,
		)
	}

	return fmt.Sprintf(
		"mcp error %d: %s (%v)",
		e.Code,
		e.Message,
		e.Data,
	)
}

func (e *MCPError) Is(target error) bool {
	if target == nil {
		return false
	}

	message := strings.ToLower(e.Message)

	switch {
	case errors.Is(target, ErrTradingDenied):
		return strings.Contains(
			message,
			"trading_access_denied",
		)

	case errors.Is(target, ErrValidation):
		return strings.Contains(
			message,
			"validation",
		)

	case errors.Is(target, ErrToolNotFound):
		return strings.Contains(
			message,
			"tool not found",
		)

	case errors.Is(target, ErrRateLimited):
		return strings.Contains(
			message,
			"rate_limited",
		)
	}

	return false
}

func (e *MCPError) AsRateLimitError() (*RateLimitError, error) {
	rateLimitErr := &RateLimitError{
		Message: e.Message,
	}

	if err := parseRateLimitMessage(e.Message, rateLimitErr); err != nil {
		return nil, err
	}

	if rateLimitErr.RetryAfter == 0 ||
		rateLimitErr.ResetAt.IsZero() ||
		rateLimitErr.Limit == 0 {
		return nil, fmt.Errorf(
			"invalid rate limit data: retry_after=%s reset_at=%s limit=%d",
			rateLimitErr.RetryAfter,
			rateLimitErr.ResetAt,
			rateLimitErr.Limit,
		)
	}

	return rateLimitErr, nil
}
