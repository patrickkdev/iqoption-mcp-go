package iqoption

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func decodeMCPToolResult(result []byte) ([]byte, error) {
	var response MCPToolResult

	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("decode MCP tool response: %w", err)
	}

	if response.IsError {
		var data json.RawMessage

		if len(response.StructuredContent) > 0 &&
			!bytes.Equal(response.StructuredContent, []byte("null")) {
			data = response.StructuredContent
		}

		for _, content := range response.Content {
			if content.Type == "text" {
				if message := strings.TrimSpace(content.Text); message != "" {
					return nil, &MCPError{
						Message: message,
						Data:    data,
					}
				}
			}
		}

		return nil, &MCPError{
			Message: "MCP tool returned an error",
			Data:    data,
		}
	}

	if len(response.StructuredContent) > 0 &&
		!bytes.Equal(response.StructuredContent, []byte("null")) {
		return response.StructuredContent, nil
	}

	for _, content := range response.Content {
		if content.Type == "text" {
			text := strings.TrimSpace(content.Text)

			if json.Valid([]byte(text)) {
				return []byte(text), nil
			}
		}
	}

	return nil, errors.New("no JSON content found in MCP tool response")
}

func decodeMCPResponse(
	body []byte,
	contentType string,
) ([]byte, error) {
	body = bytes.TrimSpace(body)

	if len(body) == 0 {
		return nil, errors.New("empty MCP response")
	}

	if json.Valid(body) {
		return body, nil
	}

	if strings.Contains(
		strings.ToLower(contentType),
		"text/event-stream",
	) {
		return decodeMCPSSE(body)
	}

	return nil, fmt.Errorf(
		"invalid MCP response: content-type=%q body=%q",
		contentType,
		summarizeBody(body),
	)
}

func decodeMCPSSE(body []byte) ([]byte, error) {
	lines := strings.Split(
		strings.ReplaceAll(
			string(body),
			"\r\n",
			"\n",
		),
		"\n",
	)

	var dataLines []string
	var sawEvent bool

	flush := func() ([]byte, bool, error) {
		if len(dataLines) == 0 {
			return nil, false, nil
		}

		data := strings.TrimSpace(
			strings.Join(dataLines, "\n"),
		)

		dataLines = nil

		if data == "" || data == "[DONE]" {
			return nil, false, nil
		}

		if !json.Valid([]byte(data)) {
			return nil, false, fmt.Errorf(
				"invalid JSON in MCP SSE data: %q",
				summarizeBody([]byte(data)),
			)
		}

		return []byte(data), true, nil
	}

	for _, line := range lines {
		// SSE comments/metadata.
		if strings.HasPrefix(line, ":") {
			continue
		}

		if after, ok := strings.CutPrefix(line, "data:"); ok {
			sawEvent = true

			// SSE permits an optional single leading space after ":".
			dataLines = append(
				dataLines,
				strings.TrimPrefix(after, " "),
			)
			continue
		}

		// A blank line terminates an SSE event.
		if line == "" {
			result, ok, err := flush()
			if err != nil {
				return nil, err
			}

			if ok {
				return result, nil
			}

			continue
		}
	}

	result, ok, err := flush()
	if err != nil {
		return nil, err
	}

	if ok {
		return result, nil
	}

	if !sawEvent {
		return nil, errors.New(
			"no SSE data events found in MCP response",
		)
	}

	return nil, errors.New(
		"no JSON content found in MCP SSE response",
	)
}
