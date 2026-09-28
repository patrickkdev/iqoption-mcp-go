package iqoption

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// RateLimitError contains information returned by an HTTP 429 response.
type RateLimitError struct {
	RetryAfter time.Duration
	ResetAt    time.Time
	Limit      int
	Message    string
}

func (e *RateLimitError) Error() string {
	if e.Message == "" {
		if e.RetryAfter > 0 {
			return fmt.Sprintf(
				"%s: retry after %s",
				ErrRateLimited,
				e.RetryAfter,
			)
		}

		return ErrRateLimited.Error()
	}

	if e.RetryAfter > 0 {
		return fmt.Sprintf(
			"%s: %s: retry after %s",
			ErrRateLimited,
			e.Message,
			e.RetryAfter,
		)
	}

	return fmt.Sprintf("%s: %s", ErrRateLimited, e.Message)
}

func (e *RateLimitError) Unwrap() error {
	return ErrRateLimited
}

var rateLimitMessageRE = regexp.MustCompile(
	`limit=(\d+),\s*retry_after_ms=(\d+),\s*reset_at=(\d+)`,
)

func parseRateLimitMessage(
	message string,
	err *RateLimitError,
) error {
	matches := rateLimitMessageRE.FindStringSubmatch(message)

	if len(matches) != 4 {
		return fmt.Errorf(
			"invalid rate limit message: %q",
			message,
		)
	}

	limit, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil {
		return fmt.Errorf(
			"invalid rate limit limit: %q: %w",
			matches[1],
			parseErr,
		)
	}

	retryAfterMS, parseErr := strconv.ParseInt(matches[2], 10, 64)
	if parseErr != nil {
		return fmt.Errorf(
			"invalid retry_after_ms: %q: %w",
			matches[2],
			parseErr,
		)
	}

	resetAtMS, parseErr := strconv.ParseInt(matches[3], 10, 64)
	if parseErr != nil {
		return fmt.Errorf(
			"invalid reset_at: %q: %w",
			matches[3],
			parseErr,
		)
	}

	err.Limit = limit
	err.RetryAfter = time.Duration(retryAfterMS) * time.Millisecond
	err.ResetAt = time.UnixMilli(resetAtMS)

	return nil
}
