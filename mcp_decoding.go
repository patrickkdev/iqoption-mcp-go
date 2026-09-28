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
