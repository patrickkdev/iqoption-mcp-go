# IQ Option MCP Client (Go)

[![Go Reference](https://pkg.go.dev/badge/github.com/patrickkdev/iqoption-mcp-go.svg)](https://pkg.go.dev/github.com/patrickkdev/iqoption-mcp-go)

Unofficial Go client library and helper toolkit for connecting applications to IQ Option through its MCP interface. It provides a typed interface for market data, account operations, position tracking, trade history, and Binary Options trading.

> **Disclaimer:** `iqoption-mcp-go` is an independent, unofficial project and is not affiliated with, endorsed by, or sponsored by IQ Option. "IQ Option" is a trademark of its respective owner.
>
> Users are responsible for complying with IQ Option's Terms & Conditions, applicable laws, and any other requirements governing their use of the IQ Option service. Trading involves financial risk.
>
> Trading involves significant financial risk, and automated trading systems can result in substantial losses. Past performance does not guarantee future results.

## Key Features

- ✅ **Account Management:** List balances and work with Practice (Training) and Real (Normal) balances.
- ✅ **Market Data:** List assets, profitability, availability, expirations, and OHLC candles.
- ✅ **Technical Analysis:** Retrieve historical and real-time candle data with typed candle sizes.
- ✅ **Trading Operations:** Place Binary Options trades using typed `CALL`/`PUT` directions.
- ✅ **Position Management:** Look up positions, sell positions, and request rollovers where supported by the MCP server.
- ✅ **Trade History:** List historical trades and retrieve a completed trade by position ID.
- ✅ **Trade Waiting:** Wait for a position to finish and obtain its completed trade result.
- ✅ **Rate-Limit Aware:** Discover server-provided limits and throttle gateway/read/write requests locally.
- ✅ **Write-Safe Retries:** Read operations may retry after rate limiting; `place_trade`, `sell_position`, and `rollover_position` are treated as writes and are not retried automatically.
- ✅ **Concurrency Safe:** Session initialization and session recovery are protected against common concurrent-use races.

## Requirements

- Access to an IQ Option MCP authentication token.

## Installation

```bash
go get github.com/patrickkdev/iqoption-mcp-go
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/patrickkdev/iqoption-mcp-go"
)

func main() {
	ctx := context.Background()

	// Initialize the client with your IQ Option MCP token.
	client, err := iqoption.New(iqoption.Config{
		Token: "YOUR_IQ_OPTION_TOKEN",
	})
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// 1. List balances.
	balances, err := client.ListBalances(ctx, iqoption.BalanceTypeTraining)
	if err != nil {
		log.Fatalf("error listing balances: %v", err)
	}

	for _, b := range balances {
		fmt.Printf("Balance [%s]: %.2f %s\n", b.Type, b.Amount, b.Currency)
	}

	// 2. Get market data.
	// Asset 1 is only an example; prefer an asset returned by ListAssets.
	candles, err := client.GetCandles(
		ctx,
		1,
		iqoption.CandleSize1Minute,
		10,
	)
	if err != nil {
		log.Fatalf("error getting candles: %v", err)
	}

	for _, c := range candles {
		fmt.Printf("Time: %v | Open: %f | Close: %f\n", c.From, c.Open, c.Close)
	}

	// 3. List available assets and expirations.
	assets, err := client.ListAssets(ctx, true)
	if err != nil {
		log.Fatalf("error listing assets: %v", err)
	}
	if len(assets) == 0 {
		log.Fatal("no tradable assets returned by the server")
	}

	for _, a := range assets {
		fmt.Printf("Asset: %s (%d) | Expirations: %v\n", a.Name, a.ID, a.Expirations)
	}

	targetAsset := assets[0]
	if len(targetAsset.Expirations) == 0 {
		log.Fatal("selected asset has no available expirations")
	}

	// 4. Place a trade.
	// Trading writes are not automatically retried by the client.
	positionID, err := client.PlaceTrade(ctx, iqoption.TradeRequest{
		BalanceID:     balances[0].BalanceID,
		AssetID:       targetAsset.ID,
		Direction:     iqoption.TradeDirectionCall,
		Amount:        1.0,
		ProfitPercent: targetAsset.ProfitPercent,
		Expired:       targetAsset.Expirations[0],
	})
	if err != nil {
		log.Fatalf("error placing trade: %v", err)
	}

	fmt.Printf("Trade accepted: position_id=%d\n", positionID)

	// 5. Wait for the trade outcome.
	tradeCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	trade, err := client.WaitForTradeResult(
		tradeCtx,
		balances[0].BalanceID,
		positionID,
		2*time.Second,
	)
	if err != nil {
		log.Fatalf("error waiting for trade result: %v", err)
	}

	fmt.Printf(
		"Trade finished: position_id=%d result=%s profit=%.2f close_reason=%s\n",
		trade.PositionID,
		trade.Result,
		trade.Profit,
		trade.CloseReason,
	)
}
```

The example uses the typed constants `CandleSize1Minute` and `TradeDirectionCall` instead of arbitrary integers and strings.

## Core API

The v1 API includes helpers for the most common account, market-data, and trading workflows.

### Market data and assets

```go
assets, err := client.ListAssets(ctx, true)

asset, err := client.GetAssetByID(ctx, 1)

asset, err := client.GetAssetByName(ctx, "EUR/USD")

candles, err := client.GetCandles(
	ctx,
	asset.ID,
	iqoption.CandleSize5Minutes,
	100,
)
```

### Positions and trade history

```go
position, err := client.GetPositionByID(ctx, positionID)

trade, err := client.GetCompletedTradeByID(ctx, positionID)

trade, err := client.WaitForTradeResult(
	ctx,
	balanceID,
	positionID,
	2*time.Second,
)
```

### Trading lifecycle

```go
positionID, err := client.PlaceTrade(ctx, iqoption.TradeRequest{
	BalanceID:     balanceID,
	AssetID:       assetID,
	Direction:     iqoption.TradeDirectionPut,
	Amount:        1,
	ProfitPercent: profitPercent,
	Expired:       expiration,
})

err = client.SellPosition(ctx, balanceID, positionID)

err = client.RolloverPosition(ctx, balanceID, positionID)
```

Trading mutation methods are deliberately treated as non-retryable writes. A transport failure after a write has been sent can be ambiguous, so the client does not automatically repeat the operation.

## Rate Limiting

The client is rate-limit aware and maintains separate local limits for:

- **Gateway** requests.
- **Read** operations.
- **Write** operations.

Server limits are discovered through the MCP `get_limits` operation. The client throttles requests locally to leave headroom and can interpret server rate-limit errors that provide retry metadata.

For rate-limited **read** operations, the client may wait and retry up to `MaxRateLimitRetries` (default: `3`). Trading writes are not retried automatically.

This means requests can be intentionally delayed even when an individual HTTP request would otherwise be accepted immediately.

## MCP Transport and Session Lifecycle

The client communicates with the upstream IQ Option MCP server using MCP/JSON-RPC over HTTP, including SSE responses where applicable.

Session handling is intentionally conservative:

1. `initialize` negotiates a session.
2. The client sends `notifications/initialized` using that session.
3. The session ID is published for normal use only after the full handshake succeeds.
4. Concurrent initialization is serialized.
5. Stale-session cleanup is guarded so an older request cannot clear a newer session.
6. `Close()` clears session and rate-limit state.

Tool results prefer MCP `structuredContent` and fall back to JSON contained in textual content when necessary.

## Error Handling

The client exposes structured errors for common transport and MCP failures, including a dedicated rate-limit error type.

Rate-limit errors can expose information such as:

- limit;
- retry-after duration;
- reset time;
- the underlying MCP/HTTP error.

Consumers that need to distinguish rate limiting can use `errors.Is(err, iqoption.ErrRateLimited)` and inspect the structured error when available.

### Advanced Usage

#### Customizing the HTTP client

You can provide your own `http.Client` for custom timeouts, proxies, transports, or other HTTP behavior:

```go
import (
	"net/http"
	"time"
)

client, err := iqoption.New(iqoption.Config{
	Token: "YOUR_IQ_OPTION_TOKEN",
	HTTPClient: &http.Client{
		Timeout: 30 * time.Second,
	},
})
```

When `HTTPClient` is omitted, the client uses a default HTTP configuration with a 60-second timeout and connection pooling.

#### Custom endpoint

By default, the client connects to:

```text
https://binary-options.mcp.iqoption.com
```

You can override the endpoint when connecting to another environment or a compatible MCP server:

```go
client, err := iqoption.New(iqoption.Config{
	Token:    "YOUR_IQ_OPTION_TOKEN",
	Endpoint: "https://example.internal/mcp",
})
```

The endpoint is trimmed and falls back to `DefaultEndpoint` when empty.

#### MCP protocol and client metadata

The MCP protocol version, client name, and client version can be customized through `Config`:

```go
client, err := iqoption.New(iqoption.Config{
	Token:           "YOUR_IQ_OPTION_TOKEN",
	ProtocolVersion: "2025-06-18",
	ClientName:      "my-trading-service",
	ClientVersion:   "2.1.0",
})
```

When omitted, these values default to:

```go
iqoption.DefaultProtocol
iqoption.DefaultClientName
iqoption.DefaultClientVersion
```

Custom client metadata can be useful when identifying different applications or deployments on the server side.

#### Rate-limit retries

The client automatically retries rate-limited requests. The maximum number of retries can be configured with `MaxRateLimitRetries`:

```go
client, err := iqoption.New(iqoption.Config{
	Token:               "YOUR_IQ_OPTION_TOKEN",
	MaxRateLimitRetries: 5,
})
```

The default is:

```go
iqoption.DefaultMaxRateLimitRetries // 3
```

Set this according to how aggressively your application should recover from temporary rate limits. A higher value may increase request latency when the server continues returning rate-limit responses.

#### Lazy session initialization

Creating a client does not make a network request:

```go
client, err := iqoption.New(iqoption.Config{
	Token: "YOUR_IQ_OPTION_TOKEN",
})

if err != nil {
	return err
}
```

The MCP session is initialized automatically when the first request is made. This makes `New` suitable for application startup code where you want to construct dependencies without establishing a connection immediately.

#### Concurrent use

`Client` is safe for concurrent use. A single client instance can therefore be shared between goroutines:

```go
client, err := iqoption.New(iqoption.Config{
	Token: "YOUR_IQ_OPTION_TOKEN",
})
if err != nil {
	return err
}

// The same client can be used by multiple goroutines.
go func() {
	// Make MCP requests...
}()

go func() {
	// Make MCP requests...
}()
```

Session establishment is internally serialized so concurrent requests do not create multiple MCP sessions unnecessarily.

#### Using a custom configuration

A complete configuration can combine all available options:

```go
client, err := iqoption.New(iqoption.Config{
	Endpoint:        "https://binary-options.mcp.iqoption.com",
	Token:           "YOUR_IQ_OPTION_TOKEN",
	ProtocolVersion: "2025-06-18",
	ClientName:      "my-iqoption-service",
	ClientVersion:   "1.2.0",
	HTTPClient: &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
		},
	},
	MaxRateLimitRetries: 5,
})
```

Only `Token` is required. All other configuration fields have sensible defaults.

### Finding expirations

The library provides helpers for finding valid server-provided expirations, including helpers such as `FindM15Expiration`.

## Testing

The project includes unit tests for the client, error handling, rate limiting, and MCP decoding, plus integration tests for live IQ Option MCP behavior.

Run the full unit-test suite with:

```bash
go test ./...
```

### Running Integration Tests

Integration tests are skipped by default unless the required environment variables are configured.

```bash
# Set your IQ Option token.
export IQOPTION_TEST_TOKEN="your_token_here"

# Enable general read-only integration tests.
export IQOPTION_LIVE_TEST=1

# Optional: enable trade integration tests.
# These tests are intended to use a Practice (TRAINING) balance.
export IQOPTION_LIVE_TRADE=1

go test -v ./...
```

`IQOPTION_LIVE_TEST=1` enables live tests for balances, assets, candles, positions, and history. `IQOPTION_LIVE_TRADE=1` enables tests that place or otherwise mutate trades and should only be used when you understand the financial and account-side effects.

## Licensing

This project is licensed under the **PolyForm Noncommercial License 1.0.0**, together with the project's **Personal Trading Exception**.

The exception permits individuals to use the software for their own personal trading, including profitable personal trading, subject to the terms of the exception.

The noncommercial license materially restricts commercial use and distribution. Review [`LICENSE`](./LICENSE) and [`LICENSE-EXCEPTION.md`](./LICENSE-EXCEPTION.md) before incorporating this library into a commercial product, paid service, SaaS offering, or software distributed to third parties.

The `NOTICE` file contains the applicable attribution notice.

## Contributing

Contributions are welcome. Please open an issue or pull request with a clear description of the change and its impact on the public API or trading behavior.

## Develop a trading bot

For developing a trading bot, [hire a trusted developer](https://patrick.makztech.com).

🇧🇷 Para desenvolver um robô de trading, [contrate um desenvolvedor de confiança](https://patrick.makztech.com).

## Keywords

`IQ Option`, `MCP`, `Model Context Protocol`, `Golang`, `Go`, `Binary Options`, `Trading Bot`, `Algorithmic Trading`, `Market Data`, `Fintech`, `Opções Binárias`, `Automação de Trades`.

---

*Disclaimer: Trading involves risk. Use this software at your own risk. The authors are not responsible for financial losses incurred through use of this software.*
