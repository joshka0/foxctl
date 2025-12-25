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

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"golang.org/x/crypto/sha3"
)

const (
	commandName = "x402/payment"

	// maxResponseSize limits HTTP response body reads to prevent memory exhaustion.
	maxResponseSize = 10 * 1024 * 1024 // 10MB

	// Operations
	OpWalletInit   = "wallet/init"
	OpWalletStatus = "wallet/status"
	OpFetch        = "fetch"
	OpPay          = "pay"

	// Wallet types
	WalletTypeCDP   = "cdp"
	WalletTypeLocal = "local"

	// Networks (CAIP-2 format internally)
	NetworkBaseMainnet   = "base-mainnet"
	NetworkBaseSepolia   = "base-sepolia"
	NetworkSolanaMainnet = "solana-mainnet"
	NetworkSolanaDevnet  = "solana-devnet"

	// USDC contract addresses
	USDCBaseMainnet = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	USDCBaseSepolia = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"

	// RPC endpoints
	RPCBaseMainnet = "https://mainnet.base.org"
	RPCBaseSepolia = "https://sepolia.base.org"
)

// Input defines the skill input parameters.
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

// Output defines the skill output.
type Output struct {
	Operation string        `json:"operation"`
	Wallet    *WalletInfo   `json:"wallet,omitempty"`
	Response  *HTTPResponse `json:"response,omitempty"`
	Payment   *PaymentInfo  `json:"payment,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// WalletInfo contains wallet details.
type WalletInfo struct {
	Address  string            `json:"address"`
	Network  string            `json:"network"`
	Type     string            `json:"type"`
	Balances map[string]string `json:"balances,omitempty"`
	CAIP2    string            `json:"caip2,omitempty"`
}

// HTTPResponse contains fetch response details.
type HTTPResponse struct {
	StatusCode    int               `json:"status_code"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          any               `json:"body,omitempty"`
	PaymentMade   bool              `json:"payment_made"`
	PaymentAmount string            `json:"payment_amount,omitempty"`
}

// PaymentInfo contains payment transaction details.
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

// PaymentRequirement from x402 protocol.
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

// WalletConfig stores wallet configuration.
type WalletConfig struct {
	Type      string `json:"type"`
	Network   string `json:"network"`
	Address   string `json:"address"`
	KeyPath   string `json:"key_path,omitempty"`
	CDPKeyID  string `json:"cdp_key_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

// JSONRPCRequest for Ethereum RPC calls.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

// JSONRPCResponse from Ethereum RPC calls.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError from JSON-RPC.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ERUNTIME", fmt.Errorf("load config: %w", err))
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ERUNTIME", fmt.Errorf("create runner context: %w", err))
	}
	defer func() { errs.Ignore(rc.Close(), "close runner context") }()

	var in Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("EARG", fmt.Errorf("decode input: %w", err))
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

	if err := run(ctx, rc, in); err != nil {
		fail("ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	switch in.Operation {
	case OpWalletInit:
		return handleWalletInit(ctx, rc, in)
	case OpWalletStatus:
		return handleWalletStatus(ctx, rc, in)
	case OpFetch:
		return handleFetch(ctx, rc, in)
	case OpPay:
		return handlePay(ctx, rc, in)
	default:
		return fmt.Errorf("unknown operation: %s (valid: wallet/init, wallet/status, fetch, pay)", in.Operation)
	}
}

func handleWalletInit(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	var wallet *WalletInfo
	var err error

	switch in.WalletType {
	case WalletTypeCDP:
		wallet, err = initCDPWallet(ctx, in.Network)
	case WalletTypeLocal:
		wallet, err = initLocalWallet(ctx, rc, in.Network, in.KeyPath)
	default:
		return fmt.Errorf("unknown wallet type: %s (valid: cdp, local)", in.WalletType)
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
			return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
		}
		return fmt.Errorf("init wallet: %w", err)
	}

	// Save wallet config
	if err := saveWalletConfig(rc, wallet, in.KeyPath); err != nil {
		return fmt.Errorf("save wallet config: %w", err)
	}

	output := Output{
		Operation: OpWalletInit,
		Wallet:    wallet,
	}

	return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
}

func handleWalletStatus(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	// Load wallet config or use provided address
	walletCfg, err := loadWalletConfig(rc)
	if err != nil && in.Address == "" {
		return fmt.Errorf("no wallet configured and no address provided: %w", err)
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
		return fmt.Errorf("no wallet address available")
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

	return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
}

func handleFetch(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	if in.URL == "" {
		return fmt.Errorf("url is required for fetch operation")
	}

	// Create HTTP request
	var bodyReader io.Reader
	if in.Body != "" {
		bodyReader = strings.NewReader(in.Body)
	}

	req, err := http.NewRequestWithContext(ctx, in.Method, in.URL, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Add headers
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}

	// Execute request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body (limited to prevent memory exhaustion)
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
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

	return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
}

func handle402Response(ctx context.Context, rc *runner.RunnerContext, in Input, resp *http.Response, body []byte) error {
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
		return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
	}

	// Decode payment requirements (base64 JSON)
	var requirements []PaymentRequirement
	if err := json.Unmarshal([]byte(paymentHeader), &requirements); err != nil {
		decoded, decErr := decodeBase64(paymentHeader)
		if decErr != nil {
			return fmt.Errorf("parse payment requirements: %w", err)
		}
		if err := json.Unmarshal(decoded, &requirements); err != nil {
			return fmt.Errorf("parse decoded payment requirements: %w", err)
		}
	}

	if len(requirements) == 0 {
		return fmt.Errorf("no payment requirements in response")
	}

	// Select a payment requirement we can fulfill
	selectedReq := selectPaymentRequirement(requirements, in.Network)
	if selectedReq == nil {
		return fmt.Errorf("no compatible payment requirement found for network %s", in.Network)
	}

	// Check max payment limit
	maxPayment, err := parseAmount(in.MaxPayment)
	if err != nil {
		return fmt.Errorf("parse max_payment: %w", err)
	}
	reqAmount, err := parseAmount(selectedReq.MaxAmountRequired)
	if err != nil {
		return fmt.Errorf("parse payment requirement amount: %w", err)
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
		return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
	}

	// Load wallet and execute payment
	walletCfg, err := loadWalletConfig(rc)
	if err != nil {
		return fmt.Errorf("load wallet for payment: %w", err)
	}

	paymentPayload, err := createPaymentPayload(ctx, walletCfg, selectedReq)
	if err != nil {
		return fmt.Errorf("create payment payload: %w", err)
	}

	// Retry request with payment signature
	var bodyReader io.Reader
	if in.Body != "" {
		bodyReader = strings.NewReader(in.Body)
	}

	retryReq, err := http.NewRequestWithContext(ctx, in.Method, in.URL, bodyReader)
	if err != nil {
		return fmt.Errorf("create retry request: %w", err)
	}

	for k, v := range in.Headers {
		retryReq.Header.Set(k, v)
	}
	retryReq.Header.Set("X-Payment", paymentPayload)

	client := &http.Client{Timeout: 30 * time.Second}
	retryResp, err := client.Do(retryReq)
	if err != nil {
		return fmt.Errorf("execute payment request: %w", err)
	}
	defer retryResp.Body.Close()

	retryBody, err := io.ReadAll(io.LimitReader(retryResp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read payment response: %w", err)
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

	return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
}

func handlePay(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	if in.To == "" {
		return fmt.Errorf("to address is required for pay operation")
	}
	if in.Amount == "" {
		return fmt.Errorf("amount is required for pay operation")
	}

	walletCfg, err := loadWalletConfig(rc)
	if err != nil {
		return fmt.Errorf("load wallet: %w", err)
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
		txHash, err := executeCDPPayment(ctx, walletCfg, in.To, in.Amount, in.Asset)
		if err != nil {
			payment.Status = "failed"
			output := Output{
				Operation: OpPay,
				Payment:   payment,
				Error:     err.Error(),
			}
			return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
		}
		payment.TxHash = txHash
		payment.Status = "submitted"

	case WalletTypeLocal:
		txHash, err := executeLocalPayment(ctx, walletCfg, in.To, in.Amount, in.Asset)
		if err != nil {
			payment.Status = "failed"
			output := Output{
				Operation: OpPay,
				Payment:   payment,
				Error:     err.Error(),
			}
			return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
		}
		payment.TxHash = txHash
		payment.Status = "submitted"

	default:
		return fmt.Errorf("unsupported wallet type: %s", walletCfg.Type)
	}

	output := Output{
		Operation: OpPay,
		Payment:   payment,
	}

	return rc.Emit(commandName, output, "application/json", envelope.Meta{Source: "run"})
}

// Wallet initialization functions

func initCDPWallet(ctx context.Context, network string) (*WalletInfo, error) {
	keyID := os.Getenv("CDP_API_KEY_ID")
	keySecret := os.Getenv("CDP_API_KEY_SECRET")

	if keyID == "" || keySecret == "" {
		return nil, fmt.Errorf("CDP wallet requires CDP_API_KEY_ID and CDP_API_KEY_SECRET environment variables. Get credentials at https://portal.cdp.coinbase.com/projects/api-keys")
	}

	// CDP wallet creation would use the coinbase-sdk-go here
	// For now, return setup instructions
	return nil, fmt.Errorf("CDP wallet integration pending. Set up credentials and use 'go get github.com/coinbase/coinbase-sdk-go' for full support")
}

func initLocalWallet(ctx context.Context, rc *runner.RunnerContext, network, keyPath string) (*WalletInfo, error) {
	var privateKey *ecdsa.PrivateKey
	var err error

	if keyPath != "" {
		// Load from file
		keyBytes, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read key file: %w", err)
		}
		keyHex := strings.TrimSpace(string(keyBytes))
		keyHex = strings.TrimPrefix(keyHex, "0x")
		privateKey, err = hexToECDSA(keyHex)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
	} else {
		// Generate new key
		privateKey, err = generateKey()
		if err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}

		// Save generated key
		keyDir := filepath.Join(os.Getenv("HOME"), ".agentctl", "keys")
		if err := os.MkdirAll(keyDir, 0o700); err != nil {
			return nil, fmt.Errorf("create key directory: %w", err)
		}

		keyPath = filepath.Join(keyDir, fmt.Sprintf("x402_%s.key", network))
		keyHex := hex.EncodeToString(privateKey.D.Bytes())
		if err := os.WriteFile(keyPath, []byte(keyHex), 0o600); err != nil {
			return nil, fmt.Errorf("save key file: %w", err)
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

func walletConfigPath(rc *runner.RunnerContext) string {
	home := os.Getenv("AGENTCTL_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".agentctl")
	}
	return filepath.Join(home, "x402_wallet.json")
}

func saveWalletConfig(rc *runner.RunnerContext, wallet *WalletInfo, keyPath string) error {
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

func loadWalletConfig(rc *runner.RunnerContext) (*WalletConfig, error) {
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
		return nil, fmt.Errorf("read wallet config: %w", err)
	}

	var cfg WalletConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse wallet config: %w", err)
	}

	return &cfg, nil
}

// Balance checking via JSON-RPC

func getBalances(ctx context.Context, address, network string) (map[string]string, error) {
	rpcURL := networkToRPC(network)
	if rpcURL == "" {
		return nil, fmt.Errorf("no RPC URL for network: %s", network)
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

	client := &http.Client{Timeout: 10 * time.Second}
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
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// Payment execution

func executeCDPPayment(ctx context.Context, wallet *WalletConfig, to, amount, asset string) (string, error) {
	return "", fmt.Errorf("CDP payment requires coinbase-sdk-go integration. See https://github.com/coinbase/coinbase-sdk-go")
}

func executeLocalPayment(ctx context.Context, wallet *WalletConfig, to, amount, asset string) (string, error) {
	if wallet.KeyPath == "" {
		return "", fmt.Errorf("local wallet requires key_path")
	}

	// For full transaction signing, we'd need to:
	// 1. Get nonce via eth_getTransactionCount
	// 2. Get gas price via eth_gasPrice
	// 3. Build and sign transaction
	// 4. Send via eth_sendRawTransaction

	return "", fmt.Errorf("local payment execution requires full transaction signing implementation. Consider using CDP wallet for managed signing")
}

func createPaymentPayload(ctx context.Context, wallet *WalletConfig, req *PaymentRequirement) (string, error) {
	// Create x402 payment payload following the protocol spec
	// For EVM, this uses ERC-3009 transferWithAuthorization
	nonce, err := generateNonce()
	if err != nil {
		return "", fmt.Errorf("generate nonce for payment: %w", err)
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

func generateKey() (*ecdsa.PrivateKey, error) {
	// Generate 32 random bytes for private key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, err
	}

	// This is a simplified implementation
	// Production should use proper secp256k1 curve
	return nil, fmt.Errorf("key generation requires crypto library - use 'go get github.com/ethereum/go-ethereum/crypto' or provide existing key via key_path")
}

func hexToECDSA(hexkey string) (*ecdsa.PrivateKey, error) {
	// Parse hex private key
	_, err := hex.DecodeString(hexkey)
	if err != nil {
		return nil, err
	}

	// This is a simplified implementation
	return nil, fmt.Errorf("ECDSA key parsing requires crypto library - use 'go get github.com/ethereum/go-ethereum/crypto'")
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

func fail(code string, err error) {
	env := envelope.Error(commandName, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure")
	os.Exit(1)
}
