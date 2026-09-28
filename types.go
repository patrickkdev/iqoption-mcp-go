package iqoption

import (
	"encoding/json"
	"time"
)

// BalanceType identifies an IQ Option balance.
type BalanceType string

const (
	BalanceTypeAll      BalanceType = "ALL"
	BalanceTypeNormal   BalanceType = "NORMAL"
	BalanceTypeTraining BalanceType = "TRAINING"
)

// AccountBalance represents an IQ Option account balance.
type AccountBalance struct {
	BalanceID   int64   `json:"balance_id"`
	Type        string  `json:"type"`
	Currency    string  `json:"currency"`
	Amount      float64 `json:"amount"`
	BonusAmount float64 `json:"bonus_amount"`
}

// Asset represents an IQ Option trading asset.
type Asset struct {
	ID                     int64
	Name                   string
	IsOpen                 bool
	Precision              int
	ProfitPercent          float64
	Expirations            []time.Time
	MinimumAmount          float64
	MaximumAmount          float64
	DeadtimeSeconds        int
	BuybackEnabled         bool
	BuybackDeadtimeSeconds int
}

type CandleSize int

const (
	CandleSize1Minute   CandleSize = 60
	CandleSize5Minutes  CandleSize = CandleSize1Minute * 5
	CandleSize15Minutes CandleSize = CandleSize1Minute * 15
	CandleSize30Minutes CandleSize = CandleSize1Minute * 30
	CandleSize1Hour     CandleSize = CandleSize1Minute * 60
	CandleSize4Hours    CandleSize = CandleSize1Hour * 4
	CandleSize1Day      CandleSize = CandleSize1Hour * 24
)

// Candle represents one OHLC candle.
type Candle struct {
	From   time.Time
	To     time.Time
	Open   float64
	Close  float64
	High   float64
	Low    float64
	Volume float64
}

// Position represents an open or recently open position.
type Position struct {
	PositionID       int64
	AssetID          int64
	AssetName        string
	Status           string
	Direction        string
	Amount           float64
	OpenPrice        float64
	CurrentPrice     float64
	ExpectedProfit   float64
	SellProfit       float64
	OpenTime         time.Time
	Expiration       time.Time
	SecondsRemaining int
}

// Trade represents a completed trade.
type Trade struct {
	PositionID int64
	AssetID    int64
	AssetName  string

	Direction string
	Amount    float64

	OpenPrice  float64
	ClosePrice float64

	Profit float64

	OpenTime  time.Time
	CloseTime time.Time

	CloseReason string
	Result      string
}

type TradeDirection string

const (
	TradeDirectionCall TradeDirection = "call"
	TradeDirectionPut  TradeDirection = "put"
)

// TradeRequest describes a trade.
//
// Expired MUST be one of the expiration timestamps returned by ListAssets.
// The server validates this.
type TradeRequest struct {
	BalanceID     int64
	AssetID       int64
	Direction     TradeDirection
	Amount        float64
	ProfitPercent float64
	Expired       time.Time
}

// InitializeResult is intentionally small because applications generally
// shouldn't depend on MCP initialization internals.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCP JSON-RPC types.

type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type MCPToolResult struct {
	Content           []MCPContent    `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

type LimitBuckets struct {
	Bucket        string `json:"bucket"`
	Limit         int    `json:"limit"`
	WindowSeconds int    `json:"window_seconds"`
}

type GetLimitsResponse struct {
	Buckets []LimitBuckets `json:"buckets"`
	Product string         `json:"product"`
	Scope   string         `json:"scope"`
	Tools   struct {
		GetCandles       string `json:"get_candles"`
		GetCapabilities  string `json:"get_capabilities"`
		GetLimits        string `json:"get_limits"`
		GetTradeHistory  string `json:"get_trade_history"`
		ListAssets       string `json:"list_assets"`
		ListBalances     string `json:"list_balances"`
		ListPositions    string `json:"list_positions"`
		PlaceTrade       string `json:"place_trade"`
		RolloverPosition string `json:"rollover_position"`
		SellPosition     string `json:"sell_position"`
	} `json:"tools"`
}
