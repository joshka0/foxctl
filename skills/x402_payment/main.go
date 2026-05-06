// Package main implements the x402/payment skill for AI-native HTTP micropayments.
//
// This skill provides wallet management and x402 protocol support for handling
// HTTP 402 Payment Required responses with cryptocurrency micropayments.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"golang.org/x/crypto/sha3"
)

const (
	commandName = "x402/payment"

	// maxResponseSize limits HTTP response body reads to prevent memory exhaustion.
	maxResponseSize = 10 * 1024 * 1024 // 10MB

	OpWalletInit   = "wallet/init"
	OpWalletStatus = "wallet/status"
	OpFetch        = "fetch"
	OpPay          = "pay"

	WalletTypeCDP   = "cdp"
	WalletTypeLocal = "local"

	NetworkBaseMainnet   = "base-mainnet"
	NetworkBaseSepolia   = "base-sepolia"
	NetworkSolanaMainnet = "solana-mainnet"
	NetworkSolanaDevnet  = "solana-devnet"

	USDCBaseMainnet = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	USDCBaseSepolia = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"

	RPCBaseMainnet = "https://mainnet.base.org"
	RPCBaseSepolia = "https://sepolia.base.org"
)

var allowedOps = []string{OpWalletInit, OpWalletStatus, OpFetch, OpPay}

// Input defines the skill input parameters for x402 payment operations.
type Input struct {
	Operation string `json:"operation"`

	// Wallet init
	WalletType string `json:"wallet_type"`
	Network    string `json:"network"`
	KeyPath    string `json:"key_path"`

	// Wallet status
	Address string `json:"address"`

	// Fetch
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	MaxPayment string            `json:"max_payment"`
	AutoPay    bool              `json:"auto_pay"`

	// Pay
	To     string `json:"to"`
	Amount string `json:"amount"`
	Asset  string `json:"asset"`
}

// Output defines the skill output with wallet, response, and payment information.
type Output struct {
	Operation string        `json:"operation"`
	Wallet    *WalletInfo   `json:"wallet,omitempty"`
	Response  *HTTPResponse `json:"response,omitempty"`
	Payment   *PaymentInfo  `json:"payment,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// WalletInfo contains wallet details with network and balance information.
type WalletInfo struct {
	Address  string            `json:"address"`
	Network  string            `json:"network"`
	Type     string            `json:"type"`
	Balances map[string]string `json:"balances,omitempty"`
	CAIP2    string            `json:"caip2,omitempty"`
}

// HTTPResponse contains fetch response details with payment status.
type HTTPResponse struct {
	StatusCode    int               `json:"status_code"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          any               `json:"body,omitempty"`
	PaymentMade   bool              `json:"payment_made"`
	PaymentAmount string            `json:"payment_amount,omitempty"`
}

// PaymentInfo contains payment transaction details with status tracking.
type PaymentInfo struct {
	TxHash    string `json:"tx_hash,omitempty"`
	From      string `json:"from"`
	To        string `json:"to"`
	Amount    string `json:"amount"`
	Asset     string `json:"asset"`
	Network   string `json:"network"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// PaymentRequirement from x402 protocol with payment terms and conditions.
type PaymentRequirement struct {
	Scheme            string `json:"scheme"`
	Network           string `json:"network"`
	MaxAmountRequired string `json:"maxAmountRequired"`
	Resource          string `json:"resource"`
	Description       string `json:"description"`
	MimeType          string `json:"mimeType"`
	PayTo             string `json:"payTo"`
	MaxTimeoutSeconds int    `json:"maxTimeoutSeconds"`
	Asset             string `json:"asset"`
	Extra             any    `json:"extra,omitempty"`
}

// WalletConfig stores wallet configuration with key management.
type WalletConfig struct {
	Type      string `json:"type"`
	Network   string `json:"network"`
	Address   string `json:"address"`
	KeyPath   string `json:"key_path,omitempty"`
	CDPKeyID  string `json:"cdp_key_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

// JSONRPCRequest for Ethereum RPC calls with standard 2.0 format.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

// JSONRPCResponse from Ethereum RPC calls with error handling.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError from JSON-RPC with code and message details.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// main is the skill entry point for x402/payment.
func main() {
	skillmain.Main(commandName, run)
}

// run orchestrates x402 payment operations with wallet management and HTTP payment handling.
//
// Index:
//
//	Purpose: Handle AI-native HTTP micropayments via x402 protocol with wallet management and automatic payment
//	Keywords: x402, micropayments, cryptocurrency, wallet_management, http_402
//	Related: handleWalletInit, handleWalletStatus, handleFetch, handlePay, createPaymentPayload
//	Flow: validate operation → set defaults → route to handler → execute wallet/fetch/pay operations → emit results
//	Resources: wallet config, JSON-RPC endpoints, HTTP client
//	Events: none
//	OutputFields: operation, wallet, response, payment, error
//
// [[protocol:x402_payment]]
// [[risk:crypto_payment_failure]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if op == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint(opHint))
	}
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

	// Set defaults
	if in.Method == "" {
		in.Method = "GET"
	}
	if in.WalletType == "" {
		in.WalletType = WalletTypeCDP
	}
	if in.Network == "" {
		in.Network = NetworkBaseSepolia
	}
	if in.MaxPayment == "" {
		in.MaxPayment = "1.00"
	}
	if in.Asset == "" {
		in.Asset = "USDC"
	}

	switch op {
	case OpWalletInit:
		return handleWalletInit(ctx, rc, in)
	case OpWalletStatus:
		return handleWalletStatus(ctx, rc, in)
	case OpFetch:
		return handleFetch(ctx, rc, in)
	case OpPay:
		return handlePay(ctx, rc, in)
	default:
		return skillerr.Arg("invalid operation", skillerr.WithHint(opHint))
	}
}

// handleWalletInit initializes wallets with support for CDP and local key management.
func handleWalletInit(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	var wallet *WalletInfo
	var err error

	switch in.WalletType {
	case WalletTypeCDP:
		wallet, err = initCDPWallet(ctx, in.Network)
	case WalletTypeLocal:
		wallet, err = initLocalWallet(ctx, rc, in.Network, in.KeyPath)
	default:
		return skillerr.Arg("unknown wallet type", skillerr.WithHint("Use wallet_type \"cdp\" or \"local\"."))
	}

	if err != nil {
		// For CDP, we'll return an informative error but still allow configuration
		if in.WalletType == WalletTypeCDP && strings.Contains(err.Error(), "requires") {
			output := Output{
				Operation: OpWalletInit,
				Wallet: &WalletInfo{
					Network: in.Network,
					Type:    WalletTypeCDP,
					CAIP2:   networkToCAIP2(in.Network),
				},
				Error: err.Error(),
			}
			return skillout.Emit(rc, commandName, output)
		}
		return skillerr.Runtime("init wallet", skillerr.WithCause(err))
	}

	// Save wallet config
	if err := saveWalletConfig(rc, wallet, in.KeyPath); err != nil {
		return skillerr.IO("save wallet config", skillerr.WithCause(err))
	}

	output := Output{
		Operation: OpWalletInit,
		Wallet:    wallet,
	}

	return skillout.Emit(rc, commandName, output)
}

// handleWalletStatus retrieves wallet information including balances via RPC queries.
func handleWalletStatus(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Load wallet config or use provided address
	walletCfg, err := loadWalletConfig(rc)
	if err != nil && in.Address == "" {
		return skillerr.Arg(
			"no wallet configured and no address provided",
			skillerr.WithCause(err),
			skillerr.WithHint("Run operation wallet/init or provide address."),
		)
	}

	address := in.Address
	network := in.Network
	walletType := "unknown"

	if walletCfg != nil && address == "" {
		address = walletCfg.Address
		network = walletCfg.Network
		walletType = walletCfg.Type
	}

	if address == "" {
		return skillerr.Arg("no wallet address available", skillerr.WithHint("Provide address or run wallet/init."))
	}

	// Get balances
	balances, err := getBalances(ctx, address, network)
	if err != nil {
		// Non-fatal: return wallet info without balances
		balances = map[string]string{"error": err.Error()}
	}

	wallet := &WalletInfo{
		Address:  address,
		Network:  network,
		Type:     walletType,
		Balances: balances,
		CAIP2:    networkToCAIP2(network),
	}

	output := Output{
		Operation: OpWalletStatus,
		Wallet:    wallet,
	}

	return skillout.Emit(rc, commandName, output)
}

// handleFetch executes HTTP requests with automatic x402 payment handling for 402 responses.
func handleFetch(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if in.URL == "" {
		return skillerr.Arg("url is required for fetch operation", skillerr.WithHint("Provide url for fetch (e.g., https://example.com)."))
	}

	// Create HTTP request
	var bodyReader io.Reader
	if in.Body != "" {
		bodyReader = strings.NewReader(in.Body)
	}

	req, err := http.NewRequestWithContext(ctx, in.Method, in.URL, bodyReader)
	if err != nil {
		return skillerr.Arg("create request", skillerr.WithCause(err))
	}

	// Add headers
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}

	// Execute request
	client := &http.Client{Timeout: 30 * time.Second}
	var resp *http.Response
	err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		resp, e = client.Do(req)
		return e
	})
	if err != nil {
		return skillerr.Integration("execute request", skillerr.WithCause(err))
	}
	defer resp.Body.Close()

	// Read response body (limited to prevent memory exhaustion)
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return skillerr.IO("read response", skillerr.WithCause(err))
	}

	// Check for 402 Payment Required
	if resp.StatusCode == http.StatusPaymentRequired && in.AutoPay {
		return handle402Response(ctx, rc, in, resp, respBody)
	}

	// Parse response body
	var bodyJSON any
	if err := json.Unmarshal(respBody, &bodyJSON); err != nil {
		bodyJSON = string(respBody)
	}

	// Build response headers
	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	output := Output{
		Operation: OpFetch,
		Response: &HTTPResponse{
			StatusCode:  resp.StatusCode,
			Headers:     headers,
			Body:        bodyJSON,
			PaymentMade: false,
		},
	}

	return skillout.Emit(rc, commandName, output)
}

// handle402Response processes HTTP 402 Payment Required responses with payment negotiation.
func handle402Response(ctx context.Context, rc *skillmain.RunContext, in Input, resp *http.Response, body []byte) error {
	// Parse x402 payment requirements from header
	paymentHeader := resp.Header.Get("X-Payment-Required")
	if paymentHeader == "" {
		paymentHeader = resp.Header.Get("Payment-Required")
	}

	if paymentHeader == "" {
		output := Output{
			Operation: OpFetch,
			Response: &HTTPResponse{
				StatusCode:  http.StatusPaymentRequired,
				Body:        string(body),
				PaymentMade: false,
			},
			Error: "received 402 but no payment requirements in response headers",
		}
		return skillout.Emit(rc, commandName, output)
	}

	// Decode payment requirements (base64 JSON)
	var requirements []PaymentRequirement
	if err := json.Unmarshal([]byte(paymentHeader), &requirements); err != nil {
		decoded, decErr := decodeBase64(paymentHeader)
		if decErr != nil {
			return skillerr.Parse("parse payment requirements", skillerr.WithCause(err))
		}
		if err := json.Unmarshal(decoded, &requirements); err != nil {
			return skillerr.Parse("parse decoded payment requirements", skillerr.WithCause(err))
		}
	}

	if len(requirements) == 0 {
		return skillerr.Validation("no payment requirements in response")
	}

	// Select a payment requirement we can fulfill
	selectedReq := selectPaymentRequirement(requirements, in.Network)
	if selectedReq == nil {
		return skillerr.Validation(fmt.Sprintf("no compatible payment requirement found for network %s", in.Network))
	}

	// Check max payment limit
	maxPayment, err := parseAmount(in.MaxPayment)
	if err != nil {
		return skillerr.Arg("parse max_payment", skillerr.WithCause(err))
	}
	reqAmount, err := parseAmount(selectedReq.MaxAmountRequired)
	if err != nil {
		return skillerr.Parse("parse payment requirement amount", skillerr.WithCause(err))
	}
	if reqAmount != nil && maxPayment != nil && reqAmount.Cmp(maxPayment) > 0 {
		output := Output{
			Operation: OpFetch,
			Response: &HTTPResponse{
				StatusCode:  http.StatusPaymentRequired,
				PaymentMade: false,
			},
			Error: fmt.Sprintf("payment amount %s exceeds max_payment %s", selectedReq.MaxAmountRequired, in.MaxPayment),
		}
		return skillout.Emit(rc, commandName, output)
	}

	// Load wallet and execute payment
	walletCfg, err := loadWalletConfig(rc)
	if err != nil {
		return skillerr.NotFound("load wallet for payment", skillerr.WithCause(err))
	}

	paymentPayload, err := createPaymentPayload(ctx, walletCfg, selectedReq)
	if err != nil {
		return skillerr.Runtime("create payment payload", skillerr.WithCause(err))
	}

	// Retry request with payment signature
	var bodyReader io.Reader
	if in.Body != "" {
		bodyReader = strings.NewReader(in.Body)
	}

	retryReq, err := http.NewRequestWithContext(ctx, in.Method, in.URL, bodyReader)
	if err != nil {
		return skillerr.Runtime("create retry request", skillerr.WithCause(err))
	}

	for k, v := range in.Headers {
		retryReq.Header.Set(k, v)
	}
	retryReq.Header.Set("X-Payment", paymentPayload)

	client := &http.Client{Timeout: 30 * time.Second}
	var retryResp *http.Response
	err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		retryResp, e = client.Do(retryReq)
		return e
	})
	if err != nil {
		return skillerr.Integration("execute payment request", skillerr.WithCause(err))
	}
	defer retryResp.Body.Close()

	retryBody, err := io.ReadAll(io.LimitReader(retryResp.Body, maxResponseSize))
	if err != nil {
		return skillerr.IO("read payment response", skillerr.WithCause(err))
	}

	var retryBodyJSON any
	if err := json.Unmarshal(retryBody, &retryBodyJSON); err != nil {
		retryBodyJSON = string(retryBody)
	}

	headers := make(map[string]string)
	for k, v := range retryResp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	output := Output{
		Operation: OpFetch,
		Response: &HTTPResponse{
			StatusCode:    retryResp.StatusCode,
			Headers:       headers,
			Body:          retryBodyJSON,
			PaymentMade:   true,
			PaymentAmount: selectedReq.MaxAmountRequired,
		},
	}

	return skillout.Emit(rc, commandName, output)
}

// handlePay executes direct cryptocurrency payments with wallet integration.
func handlePay(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if in.To == "" {
		return skillerr.Arg("to is required for pay operation", skillerr.WithHint("Provide recipient address in to."))
	}
	if in.Amount == "" {
		return skillerr.Arg("amount is required for pay operation", skillerr.WithHint("Provide amount for pay (e.g., 1.00)."))
	}

	walletCfg, err := loadWalletConfig(rc)
	if err != nil {
		return skillerr.NotFound("load wallet", skillerr.WithCause(err))
	}

	payment := &PaymentInfo{
		From:      walletCfg.Address,
		To:        in.To,
		Amount:    in.Amount,
		Asset:     in.Asset,
		Network:   walletCfg.Network,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Execute payment based on wallet type
	switch walletCfg.Type {
	case WalletTypeCDP:
		txHash, err := executeCDPPayment(ctx, walletCfg, in.To, in.Amount, in.Asset) //nolint:staticcheck // SA4023: Stub returns error
		if err != nil {                                                              //nolint:staticcheck // SA4023: Intentional
			payment.Status = "failed"
			output := Output{
				Operation: OpPay,
				Payment:   payment,
				Error:     err.Error(),
			}
			return skillout.Emit(rc, commandName, output)
		}
		payment.TxHash = txHash
		payment.Status = "submitted"

	case WalletTypeLocal:
		txHash, err := executeLocalPayment(ctx, walletCfg, in.To, in.Amount, in.Asset) //nolint:staticcheck // SA4023: Stub returns error
		if err != nil {                                                                //nolint:staticcheck // SA4023: Intentional
			payment.Status = "failed"
			output := Output{
				Operation: OpPay,
				Payment:   payment,
				Error:     err.Error(),
			}
			return skillout.Emit(rc, commandName, output)
		}
		payment.TxHash = txHash
		payment.Status = "submitted"

	default:
		return skillerr.Arg("unsupported wallet type", skillerr.WithHint("Use wallet/init to configure a supported wallet type."))
	}

	output := Output{
		Operation: OpPay,
		Payment:   payment,
	}

	return skillout.Emit(rc, commandName, output)
}

// Wallet initialization functions

func initCDPWallet(ctx context.Context, network string) (*WalletInfo, error) {
	keyID := os.Getenv("CDP_API_KEY_ID")
	keySecret := os.Getenv("CDP_API_KEY_SECRET")

	if keyID == "" || keySecret == "" {
		return nil, skillerr.Auth("CDP wallet requires CDP_API_KEY_ID and CDP_API_KEY_SECRET environment variables",
			skillerr.WithHint("Get credentials at https://portal.cdp.coinbase.com/projects/api-keys"))
	}

	// CDP wallet creation would use the coinbase-sdk-go here
	// For now, return setup instructions
	return nil, skillerr.Capability("CDP wallet integration pending",
		skillerr.WithHint("Set up credentials and use 'go get github.com/coinbase/coinbase-sdk-go' for full support"))
}

func initLocalWallet(ctx context.Context, rc *skillmain.RunContext, network, keyPath string) (*WalletInfo, error) {
	var privateKey *ecdsa.PrivateKey
	var err error

	if keyPath != "" {
		// Load from file
		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, skillerr.IO("read key file", skillerr.WithCause(err))
		}
		keyHex := strings.TrimSpace(string(keyBytes))
		keyHex = strings.TrimPrefix(keyHex, "0x")
		privateKey, err = hexToECDSA(keyHex) //nolint:staticcheck // SA4023: Stub returns error
		if err != nil {                      //nolint:staticcheck // SA4023: Intentional
			return nil, skillerr.Parse("parse private key", skillerr.WithCause(err))
		}
	} else {
		// Generate new key
		privateKey, err = generateKey() //nolint:staticcheck // SA4023: Stub returns error
		if err != nil {                 //nolint:staticcheck // SA4023: Intentional
			return nil, skillerr.Runtime("generate key", skillerr.WithCause(err))
		}

		// Save generated key
		keyDir := filepath.Join(os.Getenv("HOME"), ".foxctl", "keys")
		if err := os.MkdirAll(keyDir, 0o700); err != nil {
			return nil, skillerr.IO("create key directory", skillerr.WithCause(err))
		}

		keyPath = filepath.Join(keyDir, fmt.Sprintf("x402_%s.key", network))
		keyHex := hex.EncodeToString(privateKey.D.Bytes()) //nolint:staticcheck // SA1019: stub code, D never reached
		if err := os.WriteFile(keyPath, []byte(keyHex), 0o600); err != nil {
			return nil, skillerr.IO("save key file", skillerr.WithCause(err))
		}
	}

	address := pubkeyToAddress(&privateKey.PublicKey)

	return &WalletInfo{
		Address: address,
		Network: network,
		Type:    WalletTypeLocal,
		CAIP2:   networkToCAIP2(network),
	}, nil
}

// Config persistence

func walletConfigPath(rc *skillmain.RunContext) string {
	home := os.Getenv("FOXCTL_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".foxctl")
	}
	return filepath.Join(home, "x402_wallet.json")
}

func saveWalletConfig(rc *skillmain.RunContext, wallet *WalletInfo, keyPath string) error {
	cfg := WalletConfig{
		Type:      wallet.Type,
		Network:   wallet.Network,
		Address:   wallet.Address,
		KeyPath:   keyPath,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	configPath := walletConfigPath(rc)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0o600)
}

func loadWalletConfig(rc *skillmain.RunContext) (*WalletConfig, error) {
	// Check environment first
	if addr := os.Getenv("X402_WALLET_ADDRESS"); addr != "" {
		network := os.Getenv("X402_NETWORK")
		if network == "" {
			network = NetworkBaseSepolia
		}
		return &WalletConfig{
			Type:    "env",
			Network: network,
			Address: addr,
		}, nil
	}

	configPath := walletConfigPath(rc)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, skillerr.IO("read wallet config", skillerr.WithCause(err))
	}

	var cfg WalletConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, skillerr.Parse("parse wallet config", skillerr.WithCause(err))
	}

	return &cfg, nil
}

// Balance checking via JSON-RPC

func getBalances(ctx context.Context, address, network string) (map[string]string, error) {
	rpcURL := networkToRPC(network)
	if rpcURL == "" {
		return nil, skillerr.Arg(fmt.Sprintf("no RPC URL for network: %s", network))
	}

	balances := make(map[string]string)

	// Get ETH balance
	ethBalance, err := rpcGetBalance(ctx, rpcURL, address)
	if err == nil {
		balances["ETH"] = ethBalance
	}

	// Get USDC balance
	usdcAddr := networkToUSDC(network)
	if usdcAddr != "" {
		usdcBalance, err := rpcGetERC20Balance(ctx, rpcURL, address, usdcAddr)
		if err == nil {
			balances["USDC"] = usdcBalance
		}
	}

	return balances, nil
}

var rpcHTTPClient = &http.Client{Timeout: 10 * time.Second}

func rpcGetBalance(ctx context.Context, rpcURL, address string) (string, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBalance",
		Params:  []any{address, "latest"},
		ID:      1,
	}

	result, err := rpcCall(ctx, rpcURL, req)
	if err != nil {
		return "", err
	}

	var hexBalance string
	if err := json.Unmarshal(result, &hexBalance); err != nil {
		return "", err
	}

	// Convert hex to decimal and format as ETH
	balance := hexToBigInt(hexBalance)
	ethBalance := new(big.Float).Quo(
		new(big.Float).SetInt(balance),
		new(big.Float).SetInt(big.NewInt(1e18)),
	)
	return ethBalance.Text('f', 6), nil
}

func rpcGetERC20Balance(ctx context.Context, rpcURL, owner, token string) (string, error) {
	// balanceOf(address) selector: 0x70a08231
	ownerPadded := strings.TrimPrefix(owner, "0x")
	ownerPadded = strings.Repeat("0", 64-len(ownerPadded)) + ownerPadded
	data := "0x70a08231" + ownerPadded

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_call",
		Params: []any{
			map[string]string{
				"to":   token,
				"data": data,
			},
			"latest",
		},
		ID: 1,
	}

	result, err := rpcCall(ctx, rpcURL, req)
	if err != nil {
		return "", err
	}

	var hexBalance string
	if err := json.Unmarshal(result, &hexBalance); err != nil {
		return "", err
	}

	// USDC has 6 decimals
	balance := hexToBigInt(hexBalance)
	usdcBalance := new(big.Float).Quo(
		new(big.Float).SetInt(balance),
		new(big.Float).SetInt(big.NewInt(1e6)),
	)
	return usdcBalance.Text('f', 2), nil
}

func rpcCall(ctx context.Context, rpcURL string, req JSONRPCRequest) (json.RawMessage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := rpcHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		return nil, skillerr.Integrationf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// Payment execution

//nolint:staticcheck // SA4023: Stub implementation always returns error
func executeCDPPayment(_ context.Context, _ *WalletConfig, _, _, _ string) (string, error) {
	return "", skillerr.Capability("CDP payment requires coinbase-sdk-go integration",
		skillerr.WithHint("See https://github.com/coinbase/coinbase-sdk-go"))
}

//nolint:staticcheck // SA4023: Stub implementation always returns error
func executeLocalPayment(_ context.Context, wallet *WalletConfig, _, _, _ string) (string, error) {
	if wallet.KeyPath == "" {
		return "", skillerr.Arg("local wallet requires key_path")
	}

	// For full transaction signing, we'd need to:
	// 1. Get nonce via eth_getTransactionCount
	// 2. Get gas price via eth_gasPrice
	// 3. Build and sign transaction
	// 4. Send via eth_sendRawTransaction

	return "", skillerr.Capability("local payment execution requires full transaction signing implementation",
		skillerr.WithHint("Consider using CDP wallet for managed signing"))
}

func createPaymentPayload(ctx context.Context, wallet *WalletConfig, req *PaymentRequirement) (string, error) {
	// Create x402 payment payload following the protocol spec
	// For EVM, this uses ERC-3009 transferWithAuthorization
	nonce, err := generateNonce()
	if err != nil {
		return "", skillerr.Runtime("generate nonce for payment", skillerr.WithCause(err))
	}

	payload := map[string]any{
		"x402Version": 1,
		"scheme":      req.Scheme,
		"network":     req.Network,
		"payload": map[string]any{
			"signature": "", // Would be signed authorization
			"authorization": map[string]any{
				"from":        wallet.Address,
				"to":          req.PayTo,
				"value":       req.MaxAmountRequired,
				"validAfter":  0,
				"validBefore": time.Now().Add(time.Duration(req.MaxTimeoutSeconds) * time.Second).Unix(),
				"nonce":       nonce,
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(data), nil
}

func selectPaymentRequirement(reqs []PaymentRequirement, preferredNetwork string) *PaymentRequirement {
	caip2 := networkToCAIP2(preferredNetwork)

	// First, try to match preferred network
	for i := range reqs {
		if reqs[i].Network == caip2 || reqs[i].Network == preferredNetwork {
			return &reqs[i]
		}
	}

	// Fall back to any EVM network
	for i := range reqs {
		if strings.HasPrefix(reqs[i].Network, "eip155:") {
			return &reqs[i]
		}
	}

	// Return first if any
	if len(reqs) > 0 {
		return &reqs[0]
	}

	return nil
}

// Crypto utilities (minimal implementation without go-ethereum dependency)

//nolint:staticcheck // SA4023: Stub implementation always returns error
func generateKey() (*ecdsa.PrivateKey, error) {
	// Generate 32 random bytes for private key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, err
	}

	// This is a simplified implementation
	// Production should use proper secp256k1 curve
	return nil, skillerr.Capability("key generation requires crypto library",
		skillerr.WithHint("Use 'go get github.com/ethereum/go-ethereum/crypto' or provide existing key via key_path"))
}

//nolint:staticcheck // SA4023: Stub implementation always returns error
func hexToECDSA(hexkey string) (*ecdsa.PrivateKey, error) {
	// Parse hex private key
	_, err := hex.DecodeString(hexkey)
	if err != nil {
		return nil, err
	}

	// This is a simplified implementation
	return nil, skillerr.Capability("ECDSA key parsing requires crypto library",
		skillerr.WithHint("Use 'go get github.com/ethereum/go-ethereum/crypto'"))
}

func pubkeyToAddress(pub *ecdsa.PublicKey) string {
	// Keccak256 of public key, take last 20 bytes
	// Simplified - would need actual implementation
	return "0x0000000000000000000000000000000000000000"
}

func generateNonce() (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate random nonce: %w", err)
	}
	return "0x" + hex.EncodeToString(nonce), nil
}

// Utility functions

func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

func networkToCAIP2(network string) string {
	switch network {
	case NetworkBaseMainnet:
		return "eip155:8453"
	case NetworkBaseSepolia:
		return "eip155:84532"
	case NetworkSolanaMainnet:
		return "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	case NetworkSolanaDevnet:
		return "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"
	default:
		return network
	}
}

func networkToRPC(network string) string {
	switch network {
	case NetworkBaseMainnet:
		return RPCBaseMainnet
	case NetworkBaseSepolia:
		return RPCBaseSepolia
	default:
		return ""
	}
}

func networkToUSDC(network string) string {
	switch network {
	case NetworkBaseMainnet:
		return USDCBaseMainnet
	case NetworkBaseSepolia:
		return USDCBaseSepolia
	default:
		return ""
	}
}

func parseAmount(s string) (*big.Float, error) {
	f, _, err := big.ParseFloat(s, 10, 256, big.ToNearestEven)
	return f, err
}

func hexToBigInt(hex string) *big.Int {
	hex = strings.TrimPrefix(hex, "0x")
	n := new(big.Int)
	n.SetString(hex, 16)
	return n
}

func decodeBase64(s string) ([]byte, error) {
	if data, err := base64.StdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	return base64.URLEncoding.DecodeString(s)
}
