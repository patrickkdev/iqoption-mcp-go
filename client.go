package iqoption

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultEndpoint            = "https://binary-options.mcp.iqoption.com"
	DefaultProtocol            = "2025-06-18"
	DefaultClientName          = "iqoption-mcp-client"
	DefaultClientVersion       = "1.0.0"
	DefaultHTTPTimeout         = 60 * time.Second
	DefaultMaxRateLimitRetries = 3
)

// Config configures an IQ Option MCP client.
type Config struct {
	Endpoint string
	Token    string

	ProtocolVersion string

	ClientName    string
	ClientVersion string

	HTTPClient *http.Client

	MaxRateLimitRetries int
}

// Client is an MCP client for the IQ Option MCP server.
//
// Client is safe for concurrent use.
type Client struct {
	endpoint string
	token    string
	protocol string

	clientName    string
	clientVersion string

	httpClient *http.Client

	// initMu serializes MCP session establishment.
	//
	// It must only protect session establishment. Calls made while holding
	// this lock must never recursively call initialize().
	initMu sync.Mutex

	sessionMu sync.RWMutex
	sessionID string

	requestID atomic.Uint64

	rateLimiter         *RateLimiter
	maxRateLimitRetries int

	// Rate-limit discovery is separate from MCP session initialization.
	// This prevents GetLimits -> call -> initialize from deadlocking.
	rateLimitsMu     sync.Mutex
	rateLimitsLoaded atomic.Bool

	closeOnce sync.Once
	closed    atomic.Bool
}

// New creates a new IQ Option MCP client.
//
// No network request is made by New. The MCP session is initialized lazily
// on the first request.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("iqoption: token is required")
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}

	protocol := strings.TrimSpace(cfg.ProtocolVersion)
	if protocol == "" {
		protocol = DefaultProtocol
	}

	clientName := cfg.ClientName
	if clientName == "" {
		clientName = DefaultClientName
	}

	clientVersion := cfg.ClientVersion
	if clientVersion == "" {
		clientVersion = DefaultClientVersion
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: DefaultHTTPTimeout,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   20,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
	}

	maxRateLimitRetries := cfg.MaxRateLimitRetries
	if maxRateLimitRetries == 0 {
		maxRateLimitRetries = DefaultMaxRateLimitRetries
	}

	return &Client{
		endpoint:            endpoint,
		token:               cfg.Token,
		protocol:            protocol,
		clientName:          clientName,
		clientVersion:       clientVersion,
		httpClient:          httpClient,
		rateLimiter:         NewRateLimiter(),
		maxRateLimitRetries: maxRateLimitRetries,
	}, nil
}

// Close releases the client.
//
// The HTTP client itself is not closed because ownership belongs to Config
// when a custom HTTP client is supplied.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)

		c.sessionMu.Lock()
		c.sessionID = ""
		c.sessionMu.Unlock()

		c.rateLimitsLoaded.Store(false)
	})

	return nil
}

// IsClosed reports whether Close has been called.
func (c *Client) IsClosed() bool {
	return c.closed.Load()
}

func (c *Client) nextRequestID() uint64 {
	var buf [8]byte

	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failure is extremely unlikely. Since this method
		// currently cannot return an error, fall back to a timestamp-derived
		// value rather than returning a predictable sequential ID.
		return uint64(time.Now().UnixNano())
	}

	return binary.LittleEndian.Uint64(buf[:])
}

func (c *Client) getSessionID() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()

	return c.sessionID
}

func (c *Client) setSessionID(sessionID string) {
	c.sessionMu.Lock()
	c.sessionID = sessionID
	c.sessionMu.Unlock()
}

// clearSession clears the current session only if it is still the expected
// session. This prevents an old request from clearing a newly-established
// session.
func (c *Client) clearSession(expected string) bool {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	if c.sessionID != expected {
		return false
	}

	c.sessionID = ""
	c.rateLimitsLoaded.Store(false)

	return true
}

func (c *Client) loadRateLimitsOnce(ctx context.Context) error {
	if c.rateLimitsLoaded.Load() {
		return nil
	}

	c.rateLimitsMu.Lock()
	defer c.rateLimitsMu.Unlock()

	if c.rateLimitsLoaded.Load() {
		return nil
	}

	if c.IsClosed() {
		return ErrClosed
	}

	limits, err := c.GetLimits(ctx)
	if err != nil {
		return fmt.Errorf("load rate limits: %w", err)
	}

	gateway := 0
	read := 0
	write := 0

	var (
		gatewaySet bool
		readSet    bool
		writeSet   bool
	)

	for _, bucket := range limits {
		if bucket.Limit < 0 {
			return fmt.Errorf(
				"invalid negative rate limit for bucket %q: %d",
				bucket.Bucket,
				bucket.Limit,
			)
		}

		switch bucket.Bucket {
		case "gateway":
			gateway = bucket.Limit
			gatewaySet = true

		case "read":
			read = bucket.Limit
			readSet = true

		case "write":
			write = bucket.Limit
			writeSet = true
		}
	}

	// The server is expected to expose all three buckets. Do not silently
	// convert a malformed response into zero-valued limits.
	if !gatewaySet || !readSet || !writeSet {
		return fmt.Errorf(
			"incomplete rate-limit response: gateway=%t read=%t write=%t",
			gatewaySet,
			readSet,
			writeSet,
		)
	}

	c.rateLimiter.Update(RateLimits{
		GatewayPerMinute: gateway,
		ReadPerMinute:    read,
		WritePerMinute:   write,
	})

	c.rateLimitsLoaded.Store(true)

	return nil
}

// initialize establishes the MCP session.
//
// The session ID is deliberately not published until the complete MCP
// initialization sequence has succeeded:
//
//	initialize -> initialized notification -> publish session
//
// This prevents another goroutine from using a partially initialized session.
func (c *Client) initialize(ctx context.Context) error {
	if c.IsClosed() {
		return ErrClosed
	}

	if c.getSessionID() != "" {
		return nil
	}

	c.initMu.Lock()
	defer c.initMu.Unlock()

	// Another goroutine may have initialized the session while we were
	// waiting for initMu.
	if c.getSessionID() != "" {
		return nil
	}

	if c.IsClosed() {
		return ErrClosed
	}

	params := InitializeParams{
		ProtocolVersion: c.protocol,
		Capabilities:    map[string]any{},
		ClientInfo: ClientInfo{
			Name:    c.clientName,
			Version: c.clientVersion,
		},
	}

	// No session ID is supplied for the initial initialize request.
	result, sessionID, err := c.postWithSession(
		ctx,
		"initialize",
		params,
		false,
		"",
	)
	if err != nil {
		return fmt.Errorf("iqoption: initialize: %w", err)
	}

	if sessionID == "" {
		return errors.New(
			"iqoption: initialize response did not contain mcp-session-id",
		)
	}

	var response InitializeResult

	if err := json.Unmarshal(result, &response); err != nil {
		return fmt.Errorf(
			"iqoption: decode initialize response: %w",
			err,
		)
	}

	if response.ProtocolVersion == "" {
		return errors.New(
			"iqoption: server returned empty protocol version",
		)
	}

	// MCP requires the initialized notification after initialization.
	//
	// Use the freshly returned session ID explicitly instead of publishing it
	// to the client before the initialization sequence is complete.
	if _, _, err := c.postWithSession(
		ctx,
		"notifications/initialized",
		nil,
		true,
		sessionID,
	); err != nil {
		return fmt.Errorf(
			"iqoption: initialized notification: %w",
			err,
		)
	}

	if c.IsClosed() {
		return ErrClosed
	}

	c.setSessionID(sessionID)
	c.rateLimitsLoaded.Store(false)

	return nil
}

// call executes an MCP request.
//
// Session recovery is deliberately limited to read-only operations. Retrying
// a side-effecting trading operation after an ambiguous transport/session
// failure could execute the trade twice.
func (c *Client) call(
	ctx context.Context,
	method string,
	params any,
) ([]byte, error) {
	if c.IsClosed() {
		return nil, ErrClosed
	}

	if err := c.initialize(ctx); err != nil {
		return nil, err
	}

	result, _, err := c.post(ctx, method, params, false)
	if err == nil {
		return result, nil
	}

	if !errors.Is(err, ErrSessionExpired) {
		return nil, err
	}

	oldSessionID := c.getSessionID()
	if oldSessionID == "" {
		return nil, err
	}

	c.clearSession(oldSessionID)

	if initErr := c.initialize(ctx); initErr != nil {
		return nil, fmt.Errorf(
			"iqoption: session recovery failed: %w",
			initErr,
		)
	}

	return nil, ErrSessionExpired
}

func (c *Client) callTool(
	ctx context.Context,
	tool string,
	arguments map[string]any,
) ([]byte, error) {
	if tool != getLimitsToolName {
		if err := c.loadRateLimitsOnce(ctx); err != nil {
			return nil, err
		}
	}

	for attempt := 0; attempt <= c.maxRateLimitRetries; attempt++ {
		if c.toolIsWrite(tool) {
			if err := c.rateLimiter.Write.Wait(ctx); err != nil {
				return nil, err
			}
		} else {
			if err := c.rateLimiter.Read.Wait(ctx); err != nil {
				return nil, err
			}
		}

		result, err := c.call(
			ctx,
			"tools/call",
			ToolCallParams{
				Name:      tool,
				Arguments: arguments,
			},
		)

		if err == nil {
			result, err = decodeMCPToolResult(result)
		}

		if err == nil {
			return result, nil
		}

		var rateLimitErr *RateLimitError
		if !errors.As(err, &rateLimitErr) {
			log.Printf("unexpected error: %v", err)
			return nil, err
		}

		// Never retry writes.
		if c.toolIsWrite(tool) {
			return nil, err
		}

		if attempt >= c.maxRateLimitRetries {
			return nil, err
		}

		if err := waitRateLimitError(
			ctx,
			rateLimitErr,
		); err != nil {
			return nil, err
		}
	}

	return nil, errors.New("rate limit retry loop exhausted")
}

func waitRateLimitError(
	ctx context.Context,
	err *RateLimitError,
) error {
	delay := time.Until(err.ResetAt)

	if delay <= 0 && err.RetryAfter > 0 {
		delay = err.RetryAfter
	}

	if delay <= 0 {
		delay = time.Second
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// post sends one MCP JSON-RPC request using the currently active session.
func (c *Client) post(
	ctx context.Context,
	method string,
	params any,
	notification bool,
) ([]byte, string, error) {
	return c.postWithSession(
		ctx,
		method,
		params,
		notification,
		c.getSessionID(),
	)
}

func (c *Client) postWithSession(
	ctx context.Context,
	method string,
	params any,
	notification bool,
	sessionID string,
) ([]byte, string, error) {
	request := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	if !notification {
		request.ID = c.nextRequestID()
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, "", fmt.Errorf("encode request: %w", err)
	}

	return c.doHTTP(ctx, body, sessionID)
}

func (c *Client) toolIsWrite(tool string) bool {
	switch tool {
	case "place_trade", "rollover_position", "sell_position":
		return true
	default:
		return false
	}
}

func (c *Client) doHTTP(
	ctx context.Context,
	body []byte,
	sessionID string,
) ([]byte, string, error) {
	if c.IsClosed() {
		return nil, "", ErrClosed
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, "", fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(
		"Accept",
		"application/json, text/event-stream",
	)

	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	req.Header.Set(
		"MCP-Protocol-Version",
		c.protocol,
	)

	if err := c.rateLimiter.Gateway.Wait(ctx); err != nil {
		return nil, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", classifyNetworkError(err)
	}
	defer resp.Body.Close()

	responseSessionID := resp.Header.Get("Mcp-Session-Id")

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, responseSessionID, fmt.Errorf(
			"read HTTP response: %w",
			err,
		)
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, responseSessionID, fmt.Errorf(
			"%w: HTTP %d",
			ErrUnauthorized,
			resp.StatusCode,
		)

	case http.StatusNotFound, http.StatusGone:
		return nil, responseSessionID, ErrSessionExpired
	}

	if resp.StatusCode >= 500 {
		return nil, responseSessionID, fmt.Errorf(
			"%w: HTTP %d: %s",
			ErrServerUnavailable,
			resp.StatusCode,
			summarizeBody(responseBody),
		)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, responseSessionID, fmt.Errorf(
			"HTTP %d: %s",
			resp.StatusCode,
			summarizeBody(responseBody),
		)
	}

	// Streamable HTTP notifications are allowed to return an empty body.
	if len(bytes.TrimSpace(responseBody)) == 0 {
		return nil, responseSessionID, nil
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}

	result, err := decodeMCPResponse(responseBody, contentType)
	if err != nil {
		return nil, responseSessionID, err
	}

	var rpc JSONRPCResponse
	if err := json.Unmarshal(result, &rpc); err != nil {
		return nil, responseSessionID, fmt.Errorf(
			"decode JSON-RPC response: %w",
			err,
		)
	}

	if rpc.Error != nil {
		mcpErr := &MCPError{
			Code:    rpc.Error.Code,
			Message: rpc.Error.Message,
			Data:    rpc.Error.Data,
		}

		if mcpErr.Is(ErrRateLimited) {
			rateLimitErr, err := mcpErr.AsRateLimitError()
			if err != nil {
				return nil, responseSessionID, err
			}

			return nil, responseSessionID, rateLimitErr
		}

		return nil, responseSessionID, mcpErr
	}

	return rpc.Result, responseSessionID, nil
}

func classifyNetworkError(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrNetwork, err)
}

func summarizeBody(body []byte) string {
	const max = 500

	text := strings.TrimSpace(string(body))

	if len(text) > max {
		return text[:max] + "..."
	}

	return text
}
