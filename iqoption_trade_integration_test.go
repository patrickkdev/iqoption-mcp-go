package iqoption

import (
	"context"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLive_DemoPlaceTrade(t *testing.T) {
	if !liveTestsEnabled() {
		t.Skip(
			"live IQ Option tests disabled; " +
				"set IQOPTION_LIVE_TEST=1",
		)
	}

	if !liveTradeEnabled() {
		t.Skip(
			"demo trade test disabled; " +
				"set IQOPTION_LIVE_TRADE=1",
		)
	}

	waitForResult := liveTradeWaitResultEnabled()

	// Waiting for a trade result intentionally makes this test slow.
	// Give the trade enough time to expire and appear in history.
	timeout := 45 * time.Second
	if waitForResult {
		timeout = 20 * time.Minute
	}

	client := liveClient(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		timeout,
	)
	defer cancel()

	// Explicitly request TRAINING only.
	balances, err := client.ListBalances(
		ctx,
		BalanceTypeTraining,
	)
	if err != nil {
		t.Fatalf(
			"ListBalances(TRAINING): %v",
			err,
		)
	}

	var training *AccountBalance

	for i := range balances {
		if strings.EqualFold(
			balances[i].Type,
			"training",
		) {
			training = &balances[i]
			break
		}
	}

	if training == nil {
		t.Fatal("no training balance available")
	}

	if !strings.EqualFold(training.Type, "training") {
		t.Fatalf(
			"selected balance is not training: %q",
			training.Type,
		)
	}

	if training.BalanceID <= 0 {
		t.Fatalf(
			"invalid training balance ID: %d",
			training.BalanceID,
		)
	}

	asset := requireTradeAsset(t, ctx, client)

	amount := 10.00

	if len(asset.Expirations) == 0 {
		t.Skip(
			"no expiration available for asset",
		)
	}

	expiration := asset.Expirations[0]

	t.Logf(
		"placing DEMO trade: balance=%d asset=%s amount=%.2f expiration=%s",
		training.BalanceID,
		asset.Name,
		amount,
		expiration,
	)

	// This is intentionally a deterministic test direction rather than
	// pretending the test is evaluating a trading strategy.
	positionID, err := client.PlaceTrade(
		ctx,
		TradeRequest{
			BalanceID:     training.BalanceID,
			AssetID:       asset.ID,
			Direction:     TradeDirectionCall,
			Amount:        amount,
			ProfitPercent: asset.ProfitPercent,
			Expired:       expiration,
		},
	)
	if err != nil {
		t.Fatalf(
			"PlaceTrade(DEMO) failed: %v",
			err,
		)
	}

	if positionID <= 0 {
		t.Fatalf(
			"PlaceTrade(DEMO) returned invalid position ID: %d",
			positionID,
		)
	}

	t.Logf(
		"DEMO trade accepted: position_id=%d",
		positionID,
	)

	// Verify PositionByID/GetPositionByID directly using the position
	// returned by PlaceTrade.
	position, exists, err := client.GetPositionByID(
		ctx,
		positionID,
		training.BalanceID,
	)
	if err != nil {
		t.Fatalf(
			"GetPositionByID() after trade: %v",
			err,
		)
	}

	if !exists {
		t.Fatalf(
			"GetPositionByID() after trade: position %d not found",
			positionID,
		)
	}

	if position == nil {
		t.Fatalf(
			"GetPositionByID() after trade: position %d is nil",
			positionID,
		)
	}

	if position.PositionID != positionID {
		t.Fatalf(
			"GetPositionByID() position ID mismatch: got %d want %d",
			position.PositionID,
			positionID,
		)
	}

	if position.AssetID != asset.ID {
		t.Fatalf(
			"position %d asset mismatch: got %d want %d",
			position.PositionID,
			position.AssetID,
			asset.ID,
		)
	}

	if !strings.EqualFold(
		position.Direction,
		"call",
	) {
		t.Fatalf(
			"position %d direction mismatch: got %q",
			position.PositionID,
			position.Direction,
		)
	}

	if position.Amount != amount {
		t.Fatalf(
			"position %d amount mismatch: got %.2f want %.2f",
			position.PositionID,
			position.Amount,
			amount,
		)
	}

	t.Logf(
		"verified DEMO position by ID: id=%d status=%s expiration=%s remaining=%ds",
		position.PositionID,
		position.Status,
		position.Expiration.Format(time.RFC3339),
		position.SecondsRemaining,
	)

	if !waitForResult {
		return
	}

	t.Logf(
		"waiting for DEMO trade result: position_id=%d expiration=%s",
		positionID,
		position.Expiration.Format(time.RFC3339),
	)

	trade, err := client.WaitForTradeResult(
		ctx,
		training.BalanceID,
		positionID,
	)
	if err != nil {
		t.Fatalf(
			"WaitForTradeResult() failed: %v",
			err,
		)
	}

	if trade.PositionID == 0 {
		t.Fatalf(
			"WaitForTradeResult() returned trade with zero position ID",
		)
	}

	if trade.PositionID != positionID {
		t.Fatalf(
			"WaitForTradeResult() position ID mismatch: got %d want %d",
			trade.PositionID,
			positionID,
		)
	}

	t.Logf(
		"verified DEMO trade result: position_id=%d",
		trade.PositionID,
	)
}

func requireTradeAsset(
	t *testing.T,
	ctx context.Context,
	client *Client,
) *Asset {
	t.Helper()

	assets, err := client.ListAssets(ctx, true)
	if err != nil {
		t.Fatalf("ListAssets(): %v", err)
	}

	var eligible []*Asset

	for i := range assets {
		asset := &assets[i]

		if !asset.IsOpen ||
			asset.ProfitPercent <= 0 ||
			len(asset.Expirations) == 0 {
			continue
		}

		eligible = append(eligible, asset)
	}

	if len(eligible) == 0 {
		t.Skipf("no suitable tradable asset available: checked %d assets", len(assets))
		return nil
	}

	return eligible[rand.Intn(len(eligible))]
}

func liveTestsEnabled() bool {
	return envEnabled(liveTestEnv)
}

func liveTradeEnabled() bool {
	return envEnabled(liveTradeEnv)
}

func liveTradeWaitResultEnabled() bool {
	return envEnabled(liveTradeWaitResultEnv)
}

func envEnabled(name string) bool {
	return strings.TrimSpace(
		os.Getenv(name),
	) == "1"
}
