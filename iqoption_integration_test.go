package iqoption

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	liveTestTokenEnv = "IQOPTION_TEST_TOKEN"

	// Set IQOPTION_LIVE_TEST=1 to enable tests that actually hit IQ Option.
	liveTestEnv = "IQOPTION_LIVE_TEST"

	// Set IQOPTION_LIVE_TRADE=1 to allow the demo trade test.
	//
	// This is intentionally separate from IQOPTION_LIVE_TEST because a test
	// that merely reads account data should not be able to place an order.
	liveTradeEnv           = "IQOPTION_LIVE_TRADE"
	liveTradeWaitResultEnv = "IQOPTION_LIVE_TRADE_WAIT_RESULT"
)

func liveClient(t *testing.T) *Client {
	t.Helper()

	if os.Getenv(liveTestEnv) != "1" {
		t.Skip(
			"live IQ Option tests disabled; " +
				"set IQOPTION_LIVE_TEST=1",
		)
	}

	token := strings.TrimSpace(
		os.Getenv(liveTestTokenEnv),
	)

	if token == "" {
		t.Fatal(
			"IQOPTION_TEST_TOKEN is not configured",
		)
	}

	client, err := New(Config{
		Token:         token,
		ClientName:    "iqoption-mcp-client-integration-test",
		ClientVersion: "test",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close()
	})

	return client
}

// func TestLive_DebugTools(t *testing.T) {
// 	client := liveClient(t)

// 	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
// 	defer cancel()

// 	tools, err := client.ListTools(ctx)
// 	if err != nil {
// 		t.Fatalf("list tools: %v", err)
// 	}

// 	data, err := json.MarshalIndent(tools, "", "  ")
// 	if err != nil {
// 		t.Fatalf("marshal tools: %v", err)
// 	}

// 	fmt.Println("\n========== MCP TOOLS ==========")
// 	fmt.Println(string(data))
// 	fmt.Println("========== END MCP TOOLS ==========")
// }

func TestLive_InitializeAndCapabilities(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	capabilities, err := client.GetCapabilities(ctx)
	if err != nil {
		t.Fatalf(
			"GetCapabilities() failed: %v",
			err,
		)
	}

	if capabilities == nil {
		t.Fatal("GetCapabilities() returned nil capabilities")
	}

	t.Logf(
		"connected successfully; capabilities=%d fields",
		len(capabilities),
	)
}

func TestLive_ListBalances(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	balances, err := client.ListBalances(
		ctx,
		BalanceTypeTraining,
	)
	if err != nil {
		t.Fatalf(
			"ListBalances(TRAINING) failed: %v",
			err,
		)
	}

	if len(balances) == 0 {
		t.Fatal(
			"expected at least one training balance",
		)
	}

	var foundTraining bool

	for _, balance := range balances {
		t.Logf(
			"balance id=%d type=%s currency=%s amount=%.2f",
			balance.BalanceID,
			balance.Type,
			balance.Currency,
			balance.Amount,
		)

		if strings.EqualFold(
			balance.Type,
			"training",
		) {
			foundTraining = true

			if balance.BalanceID <= 0 {
				t.Errorf(
					"training balance has invalid id: %d",
					balance.BalanceID,
				)
			}

			if balance.Currency == "" {
				t.Error(
					"training balance has empty currency",
				)
			}
		}
	}

	if !foundTraining {
		t.Fatal(
			"server returned balances but no training balance",
		)
	}
}

func TestLive_ListAssets(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	assets, err := client.ListAssets(ctx, true)
	if err != nil {
		t.Fatalf(
			"ListAssets(true) failed: %v",
			err,
		)
	}

	if len(assets) == 0 {
		t.Fatal("expected at least one enabled asset")
	}

	for _, asset := range assets {
		if asset.ID <= 0 {
			t.Errorf(
				"asset %q has invalid ID: %d",
				asset.Name,
				asset.ID,
			)
		}

		if asset.Name == "" {
			t.Errorf(
				"asset %d has empty name",
				asset.ID,
			)
		}

		if !asset.IsOpen {
			t.Errorf(
				"ListAssets(true) returned closed asset %q",
				asset.Name,
			)
		}

		t.Logf(
			"asset id=%d name=%s open=%t profit=%.2f expirations=%d",
			asset.ID,
			asset.Name,
			asset.IsOpen,
			asset.ProfitPercent,
			len(asset.Expirations),
		)

		for _, expiration := range asset.Expirations {
			if expiration.IsZero() {
				t.Errorf(
					"asset %q contains zero expiration",
					asset.Name,
				)
			}
		}
	}
}

func TestLive_GetCandles(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		45*time.Second,
	)
	defer cancel()

	assets, err := client.ListAssets(ctx, true)
	if err != nil {
		t.Fatalf(
			"ListAssets() failed: %v",
			err,
		)
	}

	if len(assets) == 0 {
		t.Fatal("no enabled assets returned")
	}

	var asset *Asset

	for i := range assets {
		if assets[i].IsOpen &&
			len(assets[i].Expirations) > 0 {
			asset = &assets[i]
			break
		}
	}

	if asset == nil {
		t.Skip("no open asset with an expiration was available")
	}

	candles, err := client.GetCandles(
		ctx,
		asset.ID,
		CandleSize15Minutes,
		20,
	)
	if err != nil {
		t.Fatalf(
			"GetCandles(asset=%d, size=900) failed: %v",
			asset.ID,
			err,
		)
	}

	if len(candles) == 0 {
		t.Fatal("expected M15 candles")
	}

	for i, candle := range candles {
		if candle.From.IsZero() {
			t.Errorf(
				"candle %d has zero From timestamp",
				i,
			)
		}

		if candle.To.IsZero() {
			t.Errorf(
				"candle %d has zero To timestamp",
				i,
			)
		}

		if !candle.To.After(candle.From) {
			t.Errorf(
				"candle %d has invalid interval: %v -> %v",
				i,
				candle.From,
				candle.To,
			)
		}

		if candle.High < candle.Open ||
			candle.High < candle.Close ||
			candle.High < candle.Low {
			t.Errorf(
				"candle %d has invalid high: %+v",
				i,
				candle,
			)
		}

		if candle.Low > candle.Open ||
			candle.Low > candle.Close ||
			candle.Low > candle.High {
			t.Errorf(
				"candle %d has invalid low: %+v",
				i,
				candle,
			)
		}

		t.Logf(
			"candle %d from=%s open=%.8f high=%.8f low=%.8f close=%.8f",
			i,
			candle.From.Format(time.RFC3339),
			candle.Open,
			candle.High,
			candle.Low,
			candle.Close,
		)
	}
}

func TestLive_ListPositions(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	balances, err := client.ListBalances(
		ctx,
		BalanceTypeTraining,
	)
	if err != nil {
		t.Fatalf(
			"ListBalances() failed: %v",
			err,
		)
	}

	if len(balances) == 0 {
		t.Fatal("no training balances available")
	}

	positions, err := client.ListPositions(
		ctx,
		balances[0].BalanceID,
	)
	if err != nil {
		t.Fatalf(
			"ListPositions() failed: %v",
			err,
		)
	}

	for _, position := range positions {
		if position.PositionID <= 0 {
			t.Errorf(
				"position has invalid ID: %d",
				position.PositionID,
			)
		}

		if position.AssetID <= 0 {
			t.Errorf(
				"position %d has invalid asset ID",
				position.PositionID,
			)
		}

		t.Logf(
			"position id=%d asset=%s direction=%s amount=%.2f remaining=%ds",
			position.PositionID,
			position.AssetName,
			position.Direction,
			position.Amount,
			position.SecondsRemaining,
		)
	}
}

func TestLive_TradeHistory(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	trades, err := client.ListTradeHistory(
		ctx,
		0,
		20,
	)
	if err != nil {
		t.Fatalf(
			"ListTradeHistory() failed: %v",
			err,
		)
	}

	for _, trade := range trades {
		if trade.PositionID <= 0 {
			t.Errorf(
				"trade has invalid position ID: %d",
				trade.PositionID,
			)
		}

		if trade.AssetID <= 0 {
			t.Errorf(
				"trade %d has invalid asset ID",
				trade.PositionID,
			)
		}

		t.Logf(
			"trade position=%d asset=%s direction=%s amount=%.2f profit=%.2f result=%s",
			trade.PositionID,
			trade.AssetName,
			trade.Direction,
			trade.Amount,
			trade.Profit,
			trade.Result,
		)
	}
}

func TestLive_GetAssetByID(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	assets, err := client.ListAssets(ctx, true)
	if err != nil {
		t.Fatalf("ListAssets() failed: %v", err)
	}

	if len(assets) == 0 {
		t.Fatal("no enabled assets returned")
	}

	expected := assets[0]

	asset, found, err := client.GetAssetByID(
		ctx,
		expected.ID,
	)
	if err != nil {
		t.Fatalf(
			"GetAssetByID(%d) failed: %v",
			expected.ID,
			err,
		)
	}

	if !found {
		t.Fatalf(
			"GetAssetByID(%d) returned found=false",
			expected.ID,
		)
	}

	if asset == nil {
		t.Fatal("GetAssetByID() returned nil asset")
	}

	if asset.ID != expected.ID {
		t.Errorf(
			"asset ID mismatch: got %d, want %d",
			asset.ID,
			expected.ID,
		)
	}

	if asset.Name != expected.Name {
		t.Errorf(
			"asset name mismatch: got %q, want %q",
			asset.Name,
			expected.Name,
		)
	}

	t.Logf(
		"found asset id=%d name=%s open=%t",
		asset.ID,
		asset.Name,
		asset.IsOpen,
	)
}

func TestLive_GetAssetByID_NotFound(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	assets, err := client.ListAssets(ctx, false)
	if err != nil {
		t.Fatalf("ListAssets() failed: %v", err)
	}

	unknownID := int64(1)

	for _, asset := range assets {
		if asset.ID >= unknownID {
			unknownID = asset.ID + 1
		}
	}

	asset, found, err := client.GetAssetByID(
		ctx,
		unknownID,
	)
	if err != nil {
		t.Fatalf(
			"GetAssetByID(%d) failed: %v",
			unknownID,
			err,
		)
	}

	if found {
		t.Fatalf(
			"GetAssetByID(%d) returned found=true for unknown asset",
			unknownID,
		)
	}

	if asset != nil {
		t.Fatalf(
			"GetAssetByID(%d) returned asset despite found=false: %+v",
			unknownID,
			asset,
		)
	}
}

func TestLive_GetAssetByName(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	assets, err := client.ListAssets(ctx, true)
	if err != nil {
		t.Fatalf("ListAssets() failed: %v", err)
	}

	if len(assets) == 0 {
		t.Fatal("no enabled assets returned")
	}

	expected := assets[0]

	// Exercise the intentionally forgiving name matching:
	// case, whitespace, separators and non-alphanumeric characters
	// should all be ignored.
	var query strings.Builder

	for _, r := range expected.Name {
		switch {
		case r >= 'a' && r <= 'z':
			query.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z':
			query.WriteRune(r)
		case r >= '0' && r <= '9':
			query.WriteRune(r)
		default:
			query.WriteRune('_')
		}
	}

	name := "  " + query.String() + "  "

	asset, found, err := client.GetAssetByName(
		ctx,
		name,
	)
	if err != nil {
		t.Fatalf(
			"GetAssetByName(%q) failed: %v",
			name,
			err,
		)
	}

	if !found {
		t.Fatalf(
			"GetAssetByName(%q) returned found=false; expected %q",
			name,
			expected.Name,
		)
	}

	if asset == nil {
		t.Fatal("GetAssetByName() returned nil asset")
	}

	if asset.ID != expected.ID {
		t.Errorf(
			"asset ID mismatch: got %d, want %d",
			asset.ID,
			expected.ID,
		)
	}

	if asset.Name != expected.Name {
		t.Errorf(
			"asset name mismatch: got %q, want %q",
			asset.Name,
			expected.Name,
		)
	}

	t.Logf(
		"found asset name=%s id=%d using query=%q",
		asset.Name,
		asset.ID,
		name,
	)
}

func TestLive_GetAssetByName_NotFound(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	asset, found, err := client.GetAssetByName(
		ctx,
		"__definitely_not_a_real_iq_option_asset__",
	)
	if err != nil {
		t.Fatalf(
			"GetAssetByName() failed: %v",
			err,
		)
	}

	if found {
		t.Fatal(
			"GetAssetByName() returned found=true for unknown asset",
		)
	}

	if asset != nil {
		t.Fatalf(
			"GetAssetByName() returned asset despite found=false: %+v",
			asset,
		)
	}
}

func TestLive_GetAssetByName_Empty(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	asset, found, err := client.GetAssetByName(
		ctx,
		"   ---___   ",
	)
	if err != nil {
		t.Fatalf(
			"GetAssetByName(empty normalized name) failed: %v",
			err,
		)
	}

	if found {
		t.Fatal(
			"GetAssetByName() returned found=true for empty normalized name",
		)
	}

	if asset != nil {
		t.Fatalf(
			"GetAssetByName() returned asset for empty normalized name: %+v",
			asset,
		)
	}
}

func TestLive_GetLimits(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	limits, err := client.GetLimits(ctx)
	if err != nil {
		t.Fatalf(
			"GetLimits() failed: %v",
			err,
		)
	}

	if len(limits) == 0 {
		t.Fatal("expected at least one limit bucket")
	}

	for i, bucket := range limits {
		t.Logf(
			"limit bucket %d: %+v",
			i,
			bucket,
		)
	}
}

func TestLive_ListTools(t *testing.T) {
	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	result, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf(
			"ListTools() failed: %v",
			err,
		)
	}

	if len(result) == 0 {
		t.Fatal("ListTools() returned empty response")
	}

	// Verify that the response is valid JSON without making the test
	// dependent on the exact set/order of tools exposed by the server.
	var payload map[string]any

	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf(
			"ListTools() returned invalid JSON: %v\nresponse=%s",
			err,
			string(result),
		)
	}

	t.Logf(
		"ListTools() returned %d top-level fields",
		len(payload),
	)
}
