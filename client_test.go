package iqoption

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRequiresToken(t *testing.T) {
	_, err := New(Config{})

	if err == nil {
		t.Fatal("expected token validation error")
	}
}

func TestClientClose(t *testing.T) {
	client, err := New(Config{
		Token: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	if client.IsClosed() {
		t.Fatal("new client is unexpectedly closed")
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	if !client.IsClosed() {
		t.Fatal("client should be closed")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}

	ctx := context.Background()

	_, err = client.call(ctx, "test", nil)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestDecodeMCPSSE(t *testing.T) {
	body := []byte(
		"event: message\r\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\r\n" +
			"\r\n",
	)

	result, err := decodeMCPSSE(body)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any

	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}

	if got["jsonrpc"] != "2.0" {
		t.Fatalf("unexpected response: %v", got)
	}
}

func TestDecodeMCPSSERejectsInvalidJSON(t *testing.T) {
	body := []byte(
		"data: {invalid-json}\n\n",
	)

	_, err := decodeMCPSSE(body)
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestDecodeMCPSSEMultipleDataLines(t *testing.T) {
	body := []byte(
		"data: {\"jsonrpc\":\"2.0\",\n" +
			"data: \"id\":1}\n\n",
	)

	result, err := decodeMCPSSE(body)
	if err != nil {
		t.Fatal(err)
	}

	expected := `{"jsonrpc":"2.0",
"id":1}`

	if string(result) != expected {
		t.Fatalf(
			"unexpected SSE result:\n%s",
			result,
		)
	}
}

func TestDecodeMCPResponseJSON(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)

	result, err := decodeMCPResponse(
		body,
		"application/json",
	)
	if err != nil {
		t.Fatal(err)
	}

	if string(result) != string(body) {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestDecodeMCPResponseSSE(t *testing.T) {
	body := []byte(
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
	)

	result, err := decodeMCPResponse(
		body,
		"text/event-stream",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !json.Valid(result) {
		t.Fatalf("result is not JSON: %s", result)
	}
}

func TestRateLimitError(t *testing.T) {
	err := newRateLimitError(
		"5",
		"too many requests",
	)

	if !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected ErrRateLimited")
	}

	if err.RetryAfter != 5*time.Second {
		t.Fatalf(
			"RetryAfter=%s, want 5s",
			err.RetryAfter,
		)
	}
}

func TestInitializeOnlyHappensOnceConcurrently(t *testing.T) {
	var initializeCalls atomic.Int32
	var initializedCalls atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			var request JSONRPCRequest

			if err := json.NewDecoder(
				r.Body,
			).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			switch request.Method {
			case "initialize":
				initializeCalls.Add(1)

				w.Header().Set(
					"Mcp-Session-Id",
					"test-session",
				)
				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = w.Write([]byte(`{
					"jsonrpc":"2.0",
					"id":1,
					"result":{
						"protocolVersion":"2025-06-18"
					}
				}`))

			case "notifications/initialized":
				initializedCalls.Add(1)

				if got := r.Header.Get(
					"Mcp-Session-Id",
				); got != "test-session" {
					t.Errorf(
						"initialized notification session=%q",
						got,
					)
				}

				w.WriteHeader(http.StatusAccepted)

			default:
				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = w.Write([]byte(`{
					"jsonrpc":"2.0",
					"id":1,
					"result":{}
				}`))
			}
		}),
	)
	defer server.Close()

	client, err := New(Config{
		Endpoint: server.URL,
		Token:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			if err := client.initialize(
				context.Background(),
			); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("initialize failed: %v", err)
	}

	if got := initializeCalls.Load(); got != 1 {
		t.Fatalf(
			"initialize calls=%d, want 1",
			got,
		)
	}

	if got := initializedCalls.Load(); got != 1 {
		t.Fatalf(
			"initialized calls=%d, want 1",
			got,
		)
	}

	if got := client.getSessionID(); got != "test-session" {
		t.Fatalf(
			"session=%q, want test-session",
			got,
		)
	}
}

func TestInitializeDoesNotPublishSessionBeforeInitialized(t *testing.T) {
	initialized := make(chan struct{})
	continueInitialized := make(chan struct{})

	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			var request JSONRPCRequest

			if err := json.NewDecoder(
				r.Body,
			).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			switch request.Method {
			case "initialize":
				w.Header().Set(
					"Mcp-Session-Id",
					"test-session",
				)
				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = w.Write([]byte(`{
					"jsonrpc":"2.0",
					"id":1,
					"result":{
						"protocolVersion":"2025-06-18"
					}
				}`))

			case "notifications/initialized":
				close(initialized)
				<-continueInitialized

				w.WriteHeader(http.StatusAccepted)

			default:
				w.Header().Set(
					"Content-Type",
					"application/json",
				)
				_, _ = w.Write([]byte(`{
					"jsonrpc":"2.0",
					"id":1,
					"result":{}
				}`))
			}
		}),
	)
	defer server.Close()

	client, err := New(Config{
		Endpoint: server.URL,
		Token:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)

	go func() {
		done <- client.initialize(
			context.Background(),
		)
	}()

	select {
	case <-initialized:
	case <-time.After(time.Second):
		t.Fatal(
			"initialized notification was not sent",
		)
	}

	if got := client.getSessionID(); got != "" {
		t.Fatalf(
			"session was published before initialized: %q",
			got,
		)
	}

	close(continueInitialized)

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("initialize did not complete")
	}

	if got := client.getSessionID(); got != "test-session" {
		t.Fatalf(
			"session=%q, want test-session",
			got,
		)
	}
}

func TestSessionRecoveryDoesNotRetryTrade(t *testing.T) {
	var tradeCalls atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			var request JSONRPCRequest

			if err := json.NewDecoder(
				r.Body,
			).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			switch request.Method {
			case "initialize":
				w.Header().Set(
					"Mcp-Session-Id",
					"session",
				)
				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				_, _ = w.Write([]byte(`{
					"jsonrpc":"2.0",
					"id":1,
					"result":{
						"protocolVersion":"2025-06-18"
					}
				}`))

			case "notifications/initialized":
				w.WriteHeader(http.StatusAccepted)

			case "tools/call":
				tradeCalls.Add(1)

				// Simulate an ambiguous session failure.
				w.WriteHeader(http.StatusGone)

			default:
				w.Header().Set(
					"Content-Type",
					"application/json",
				)
				_, _ = w.Write([]byte(`{
					"jsonrpc":"2.0",
					"id":1,
					"result":{}
				}`))
			}
		}),
	)
	defer server.Close()

	client, err := New(Config{
		Endpoint: server.URL,
		Token:    "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.call(
		context.Background(),
		"tools/call",
		ToolCallParams{
			Name:      "place_trade",
			Arguments: map[string]any{},
		},
	)

	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf(
			"expected ErrSessionExpired, got %v",
			err,
		)
	}

	if got := tradeCalls.Load(); got != 1 {
		t.Fatalf(
			"trade calls=%d, want exactly 1",
			got,
		)
	}
}

func TestClearSessionDoesNotClearNewSession(t *testing.T) {
	client, err := New(Config{
		Token: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	client.setSessionID("new-session")

	if cleared := client.clearSession("old-session"); cleared {
		t.Fatal("old session unexpectedly cleared new session")
	}

	if got := client.getSessionID(); got != "new-session" {
		t.Fatalf(
			"session=%q, want new-session",
			got,
		)
	}
}

func TestClassifyNetworkErrorPreservesCause(t *testing.T) {
	cause := errors.New("connection reset")

	err := classifyNetworkError(cause)

	if !errors.Is(err, ErrNetwork) {
		t.Fatal("expected ErrNetwork")
	}

	if !errors.Is(err, cause) {
		t.Fatal("underlying network error was not preserved")
	}
}

func TestSummarizeBody(t *testing.T) {
	short := "hello"

	if got := summarizeBody([]byte(short)); got != short {
		t.Fatalf("got %q, want %q", got, short)
	}

	long := strings.Repeat("x", 600)
	got := summarizeBody([]byte(long))

	if len(got) != 503 {
		t.Fatalf(
			"summary length=%d, want 503",
			len(got),
		)
	}

	if !strings.HasSuffix(got, "...") {
		t.Fatal("long summary should end with ...")
	}
}
