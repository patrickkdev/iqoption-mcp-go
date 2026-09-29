package iqoption

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ListBalances returns IQ Option balances.
func (c *Client) ListBalances(
	ctx context.Context,
	types BalanceType,
) ([]AccountBalance, error) {
	switch types {
	case BalanceTypeAll, BalanceTypeNormal, BalanceTypeTraining:
		// valid
	default:
		return nil, fmt.Errorf("unsupported balance type: %s", types)
	}

	result, err := c.callTool(
		ctx,
		"list_balances",
		map[string]any{
			"types": string(types),
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list balances: %w",
			err,
		)
	}

	var response struct {
		Balances []AccountBalance `json:"balances"`
	}

	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf(
			"decode balances: %w",
			err,
		)
	}

	return response.Balances, nil
}

// ListAssets returns currently available assets.
//
// onlyEnabled should normally be true for trading applications.
func (c *Client) ListAssets(
	ctx context.Context,
	onlyEnabled bool,
) ([]Asset, error) {
	result, err := c.callTool(
		ctx,
		"list_assets",
		map[string]any{
			"only_enabled": onlyEnabled,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}

	var payload struct {
		Assets []assetRow `json:"assets"`
	}

	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode list_assets: %w", err)
	}

	assets := make([]Asset, 0, len(payload.Assets))

	for _, row := range payload.Assets {
		asset := Asset{
			ID:                     row.AssetID,
			Name:                   row.Name,
			IsOpen:                 row.IsOpen,
			Precision:              row.Precision,
			ProfitPercent:          row.ProfitPercent,
			MinimumAmount:          row.MinimumAmount,
			MaximumAmount:          row.MaximumAmount,
			DeadtimeSeconds:        row.DeadtimeSeconds,
			BuybackEnabled:         row.BuybackEnabled,
			BuybackDeadtimeSeconds: row.BuybackDeadtimeSeconds,
		}

		for _, unix := range row.Expirations {
			asset.Expirations = append(
				asset.Expirations,
				time.Unix(unix, 0),
			)
		}

		assets = append(assets, asset)
	}

	return assets, nil
}

// GetCandles retrieves OHLC candles.
//
// size must be one of the sizes accepted by the IQ Option MCP server.
// For M15 use size=900.
func (c *Client) GetCandles(
	ctx context.Context,
	assetID int64,
	size CandleSize,
	count int,
) ([]Candle, error) {
	if assetID <= 0 {
		return nil, errors.New("get candles: invalid assetID")
	}

	if count <= 0 {
		return nil, errors.New("get candles: invalid count")
	}

	if size <= 0 {
		return nil, errors.New("get candles: invalid size")
	}

	if count > 1000 {
		count = 1000
	}

	result, err := c.callTool(
		ctx,
		"get_candles",
		map[string]any{
			"asset_id": assetID,
			"size":     size,
			"count":    count,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get candles: %w", err)
	}

	var payload struct {
		Candles []candleRow `json:"candles"`
	}

	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode get_candles: %w", err)
	}

	candles := make([]Candle, 0, len(payload.Candles))

	for _, row := range payload.Candles {
		candles = append(candles, Candle{
			From:   row.From,
			To:     row.To,
			Open:   row.Open,
			Close:  row.Close,
			High:   row.High,
			Low:    row.Low,
			Volume: row.Volume,
		})
	}

	return candles, nil
}

func find[T any](
	items []T,
	match func(T) bool,
) (T, bool) {
	for _, item := range items {
		if match(item) {
			return item, true
		}
	}

	var zero T
	return zero, false
}

// GetPositionByID returns an open position by ID.
func (c *Client) GetPositionByID(
	ctx context.Context,
	positionID int64,
	balanceID int64,
) (*Position, bool, error) {
	positions, err := c.ListPositions(ctx, balanceID)
	if err != nil {
		return nil, false, err
	}

	position, found := find(positions, func(position Position) bool {
		return position.PositionID == positionID
	})

	if !found {
		return nil, false, nil
	}

	return &position, true, nil
}

// GetAssetByID returns an asset by ID.
func (c *Client) GetAssetByID(
	ctx context.Context,
	assetID int64,
) (*Asset, bool, error) {
	assets, err := c.ListAssets(ctx, false)
	if err != nil {
		return nil, false, err
	}

	asset, found := find(assets, func(asset Asset) bool {
		return asset.ID == assetID
	})

	if !found {
		return nil, false, nil
	}

	return &asset, true, nil
}

// GetAssetByName returns an asset matching name.
//
// Matching is intentionally forgiving: case, whitespace, separators and
// other non-alphanumeric characters are ignored.
func (c *Client) GetAssetByName(
	ctx context.Context,
	name string,
) (*Asset, bool, error) {
	assets, err := c.ListAssets(ctx, false)
	if err != nil {
		return nil, false, err
	}

	target := cleanAssetName(name)
	if target == "" {
		return nil, false, nil
	}

	asset, found := find(assets, func(asset Asset) bool {
		return cleanAssetName(asset.Name) == target
	})

	if !found {
		return nil, false, nil
	}

	return &asset, true, nil
}

func cleanAssetName(name string) string {
	var b strings.Builder
	b.Grow(len(name))

	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// GetHistoryByID returns a completed trade by position ID.
//
// History is paginated until the trade is found or there are no more items.
func (c *Client) GetCompletedTradeByID(
	ctx context.Context,
	positionID int64,
) (*Trade, bool, error) {
	const pageSize = 100

	for skip := 0; ; skip += pageSize {
		history, err := c.ListTradeHistory(ctx, skip, pageSize)
		if err != nil {
			return nil, false, err
		}

		trade, found := find(history, func(trade Trade) bool {
			return trade.PositionID == positionID
		})

		if found {
			return &trade, true, nil
		}

		if len(history) < pageSize {
			return nil, false, nil
		}
	}
}

func (c *Client) WaitForTradeResult(
	ctx context.Context,
	balanceID int64,
	positionID int64,
) (Trade, error) {
	if balanceID <= 0 {
		return Trade{}, fmt.Errorf("invalid balanceID")
	}

	if positionID <= 0 {
		return Trade{}, fmt.Errorf("invalid positionID")
	}

	position, exists, err := c.GetPositionByID(ctx, balanceID, positionID)
	if err != nil {
		return Trade{}, fmt.Errorf("check trade position: %w", err)
	}

	if exists {
		if err := c.waitUntil(ctx, position.Expiration); err != nil {
			return Trade{}, fmt.Errorf("wait for trade expiration: %w", err)
		}
	}

	return c.waitForTradeHistory(ctx, positionID)
}

func (c *Client) waitUntil(ctx context.Context, t time.Time) error {
	wait := time.Until(t)

	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) waitForTradeHistory(
	ctx context.Context,
	positionID int64,
) (Trade, error) {
	for {
		if ctx.Err() != nil {
			return Trade{}, fmt.Errorf("context error: %w", ctx.Err())
		}

		history, err := c.ListTradeHistory(ctx, 0, 50) // 50 should be enough to find the trade
		if err != nil {
			return Trade{}, fmt.Errorf(
				"check trade history %d: %w",
				positionID,
				err,
			)
		}

		trade, found := find(history, func(trade Trade) bool {
			return trade.PositionID == positionID
		})

		if found {
			return trade, nil
		}
	}
}

// ListPositions returns currently open positions for a balance.
func (c *Client) ListPositions(
	ctx context.Context,
	balanceID int64,
) ([]Position, error) {
	if balanceID <= 0 {
		return nil, errors.New("list positions: balanceID must be positive")
	}

	result, err := c.callTool(
		ctx,
		"list_positions",
		map[string]any{
			"balance_id": balanceID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}

	var payload struct {
		Positions []positionRow `json:"positions"`
	}

	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode list_positions: %w", err)
	}

	positions := make([]Position, 0, len(payload.Positions))

	for _, row := range payload.Positions {
		positions = append(positions, Position{
			PositionID:       row.PositionID,
			AssetID:          row.AssetID,
			AssetName:        row.AssetName,
			Status:           row.Status,
			Direction:        row.Direction,
			Amount:           row.Amount,
			OpenPrice:        row.OpenPrice,
			CurrentPrice:     row.CurrentPrice,
			ExpectedProfit:   row.ExpectedProfit,
			SellProfit:       row.SellProfit,
			OpenTime:         row.OpenTime,
			Expiration:       row.Expiration,
			SecondsRemaining: row.SecondsRemaining,
		})
	}

	return positions, nil
}

// ListTradeHistory returns completed trades.
func (c *Client) ListTradeHistory(
	ctx context.Context,
	skip int,
	limit int,
) ([]Trade, error) {
	if skip < 0 {
		skip = 0
	}

	if limit <= 0 {
		limit = 100
	}

	result, err := c.callTool(
		ctx,
		"get_trade_history",
		map[string]any{
			"skip":  skip,
			"limit": limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get trade history: %w", err)
	}

	var payload struct {
		History []tradeRow `json:"history"`
	}

	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode get_trade_history: %w", err)
	}

	trades := make([]Trade, 0, len(payload.History))

	for _, row := range payload.History {
		trades = append(trades, Trade{
			PositionID:  row.PositionID,
			AssetID:     row.AssetID,
			AssetName:   row.AssetName,
			Direction:   row.Direction,
			Amount:      row.Amount,
			OpenPrice:   row.OpenPrice,
			ClosePrice:  row.ClosePrice,
			Profit:      row.Profit,
			OpenTime:    row.OpenTime,
			CloseTime:   row.CloseTime,
			CloseReason: row.CloseReason,
			Result:      row.Result,
		})
	}

	return trades, nil
}

// PlaceTrade submits a trade.
//
// For REAL/regular balances, the upstream MCP server requires explicit user
// confirmation before the write. This package does not bypass that requirement.
func (c *Client) PlaceTrade(
	ctx context.Context,
	req TradeRequest,
) (int64, error) {
	if req.BalanceID <= 0 {
		return 0, errors.New("place trade: balanceID must be positive")
	}

	if req.AssetID <= 0 {
		return 0, errors.New("place trade: assetID must be positive")
	}

	if req.Direction != TradeDirectionCall && req.Direction != TradeDirectionPut {
		return 0, fmt.Errorf(
			"place trade: invalid direction %q",
			req.Direction,
		)
	}

	if req.Amount <= 0 {
		return 0, errors.New("place trade: amount must be positive")
	}

	if req.ProfitPercent <= 0 {
		return 0, errors.New(
			"place trade: profit percent must be positive",
		)
	}

	if req.Expired.IsZero() {
		return 0, errors.New(
			"place trade: expiration is required",
		)
	}

	result, err := c.callTool(
		ctx,
		"place_trade",
		map[string]any{
			"balance_id":     req.BalanceID,
			"asset_id":       req.AssetID,
			"direction":      req.Direction,
			"amount":         req.Amount,
			"profit_percent": req.ProfitPercent,
			"expired":        req.Expired.Unix(),
		},
	)
	if err != nil {
		return 0, fmt.Errorf("place trade: %w", err)
	}

	var response struct {
		PositionID int64 `json:"position_id"`
	}

	if err := json.Unmarshal(result, &response); err != nil {
		return 0, fmt.Errorf("decode trade result: %w", err)
	}

	if response.PositionID <= 0 {
		return 0, errors.New(
			"place trade: server returned invalid position_id",
		)
	}

	return response.PositionID, nil
}

// SellPosition closes an open position early at the current buyback price.
//
// This is a destructive write operation. The caller should ensure that the
// position is eligible for early sale before calling this method.
func (c *Client) SellPosition(
	ctx context.Context,
	positionID int64,
) error {
	if positionID <= 0 {
		return errors.New(
			"sell position: positionID must be positive",
		)
	}

	_, err := c.callTool(
		ctx,
		"sell_position",
		map[string]any{
			"position_id": positionID,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"sell position %d: %w",
			positionID,
			err,
		)
	}

	return nil
}

// RolloverPosition moves an open losing position to the next expiration
// window.
//
// expirationTime must be the position's current expiration timestamp,
// expressed as Unix seconds. The timestamp should come directly from the
// position returned by ListPositions.
func (c *Client) RolloverPosition(
	ctx context.Context,
	positionID int64,
	expirationTime time.Time,
) (int64, error) {
	if positionID <= 0 {
		return 0, errors.New(
			"rollover position: positionID must be positive",
		)
	}

	if expirationTime.IsZero() {
		return 0, errors.New(
			"rollover position: expirationTime is required",
		)
	}

	result, err := c.callTool(
		ctx,
		"rollover_position",
		map[string]any{
			"position_id":     positionID,
			"expiration_time": expirationTime.Unix(),
		},
	)
	if err != nil {
		return 0, fmt.Errorf(
			"rollover position %d: %w",
			positionID,
			err,
		)
	}

	var response struct {
		PositionID int64 `json:"position_id"`
	}

	if err := json.Unmarshal(result, &response); err != nil {
		return 0, fmt.Errorf(
			"decode rollover position result: %w",
			err,
		)
	}

	if response.PositionID <= 0 {
		return 0, errors.New(
			"rollover position: server returned invalid position_id",
		)
	}

	return response.PositionID, nil
}

// GetCapabilities retrieves server capabilities.
func (c *Client) GetCapabilities(
	ctx context.Context,
) (map[string]any, error) {
	result, err := c.callTool(
		ctx,
		"get_capabilities",
		map[string]any{},
	)
	if err != nil {
		return nil, fmt.Errorf("get capabilities: %w", err)
	}

	var capabilities map[string]any

	if err := json.Unmarshal(result, &capabilities); err != nil {
		return nil, fmt.Errorf(
			"decode capabilities: %w",
			err,
		)
	}

	return capabilities, nil
}

const getLimitsToolName = "get_limits"

// GetLimits retrieves server limits.
func (c *Client) GetLimits(
	ctx context.Context,
) ([]LimitBuckets, error) {
	result, err := c.callTool(
		ctx,
		getLimitsToolName,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("get limits: %w", err)
	}

	var limits GetLimitsResponse
	if err := json.Unmarshal(result, &limits); err != nil {
		return nil, fmt.Errorf("decode limits: %w", err)
	}

	return limits.Buckets, nil
}

func (c *Client) ListTools(ctx context.Context) ([]byte, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	return result, nil
}

type assetRow struct {
	AssetID                int64   `json:"asset_id"`
	Name                   string  `json:"name"`
	IsOpen                 bool    `json:"is_open"`
	Precision              int     `json:"precision"`
	ProfitPercent          float64 `json:"profit_percent"`
	Expirations            []int64 `json:"expirations"`
	MinimumAmount          float64 `json:"minimum_amount"`
	MaximumAmount          float64 `json:"maximum_amount"`
	DeadtimeSeconds        int     `json:"deadtime_seconds"`
	BuybackEnabled         bool    `json:"buyback_enabled"`
	BuybackDeadtimeSeconds int     `json:"buyback_deadtime_seconds"`
}

type candleRow struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Open   float64   `json:"open"`
	Close  float64   `json:"close"`
	High   float64   `json:"max"`
	Low    float64   `json:"min"`
	Volume float64   `json:"volume"`
}

type positionRow struct {
	PositionID       int64     `json:"position_id"`
	AssetID          int64     `json:"asset_id"`
	AssetName        string    `json:"asset_name"`
	Status           string    `json:"status"`
	Direction        string    `json:"direction"`
	Amount           float64   `json:"amount"`
	OpenPrice        float64   `json:"open_price"`
	CurrentPrice     float64   `json:"current_price"`
	ExpectedProfit   float64   `json:"expected_profit"`
	SellProfit       float64   `json:"sell_profit"`
	OpenTime         time.Time `json:"open_time"`
	Expiration       time.Time `json:"expiration"`
	SecondsRemaining int       `json:"seconds_remaining"`
}

type tradeRow struct {
	PositionID  int64     `json:"position_id"`
	AssetID     int64     `json:"asset_id"`
	AssetName   string    `json:"asset_name"`
	Direction   string    `json:"direction"`
	Amount      float64   `json:"amount"`
	OpenPrice   float64   `json:"open_price"`
	ClosePrice  float64   `json:"close_price"`
	Profit      float64   `json:"profit"`
	OpenTime    time.Time `json:"open_time"`
	CloseTime   time.Time `json:"close_time"`
	CloseReason string    `json:"close_reason"`
	Result      string    `json:"result"`
}
