package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Network Mapping Tests
// =============================================================================

func TestNetworkToCAIP2(t *testing.T) {
	tests := []struct {
		name    string
		network string
		want    string
	}{
		{"base mainnet", NetworkBaseMainnet, "eip155:8453"},
		{"base sepolia", NetworkBaseSepolia, "eip155:84532"},
		{"solana mainnet", NetworkSolanaMainnet, "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"},
		{"solana devnet", NetworkSolanaDevnet, "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"},
		{"custom network", "custom-network", "custom-network"},
		{"empty string", "", ""},
		{"arbitrary string", "eip155:1", "eip155:1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := networkToCAIP2(tt.network)
			if got != tt.want {
				t.Errorf("networkToCAIP2(%q) = %q, want %q", tt.network, got, tt.want)
			}
		})
	}
}

func TestNetworkToRPC(t *testing.T) {
	tests := []struct {
		name    string
		network string
		want    string
	}{
		{"base mainnet", NetworkBaseMainnet, RPCBaseMainnet},
		{"base sepolia", NetworkBaseSepolia, RPCBaseSepolia},
		{"unknown network", "unknown", ""},
		{"empty string", "", ""},
		{"solana mainnet", NetworkSolanaMainnet, ""},
		{"solana devnet", NetworkSolanaDevnet, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := networkToRPC(tt.network)
			if got != tt.want {
				t.Errorf("networkToRPC(%q) = %q, want %q", tt.network, got, tt.want)
			}
		})
	}
}

func TestNetworkToUSDC(t *testing.T) {
	tests := []struct {
		name    string
		network string
		want    string
	}{
		{"base mainnet", NetworkBaseMainnet, USDCBaseMainnet},
		{"base sepolia", NetworkBaseSepolia, USDCBaseSepolia},
		{"unknown network", "unknown", ""},
		{"empty string", "", ""},
		{"solana mainnet", NetworkSolanaMainnet, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := networkToUSDC(tt.network)
			if got != tt.want {
				t.Errorf("networkToUSDC(%q) = %q, want %q", tt.network, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Amount Parsing Tests
// =============================================================================

func TestParseAmount(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    string // expected string representation
	}{
		{"integer", "100", false, "100"},
		{"decimal", "1.5", false, "1.5"},
		{"small decimal", "0.001", false, "0.001"},
		{"large number", "1000000.123456", false, "1000000.123456"},
		{"zero", "0", false, "0"},
		{"negative", "-1.5", false, "-1.5"},
		{"invalid string", "invalid", true, ""},
		{"empty string", "", true, ""},
		{"with spaces", " 1.5 ", true, ""},
		{"scientific notation", "1e10", false, "10000000000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAmount(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAmount(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == nil {
				t.Errorf("parseAmount(%q) returned nil without error", tt.input)
				return
			}
			if !tt.wantErr {
				// Compare as floats to handle scientific notation
				gotFloat, _, _ := big.ParseFloat(got.Text('f', -1), 10, 256, big.ToNearestEven)
				wantFloat, _, _ := big.ParseFloat(tt.want, 10, 256, big.ToNearestEven)
				if gotFloat.Cmp(wantFloat) != 0 {
					t.Errorf("parseAmount(%q) = %s, want %s", tt.input, got.Text('f', -1), tt.want)
				}
			}
		})
	}
}

func TestAmountComparison(t *testing.T) {
	tests := []struct {
		name   string
		a      string
		b      string
		wantGt bool // a > b
	}{
		{"equal", "1.00", "1.00", false},
		{"greater", "2.00", "1.00", true},
		{"less", "0.50", "1.00", false},
		{"small difference", "1.001", "1.000", true},
		{"large vs small", "1000.00", "0.01", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := parseAmount(tt.a)
			b, _ := parseAmount(tt.b)
			got := a.Cmp(b) > 0
			if got != tt.wantGt {
				t.Errorf("(%s > %s) = %v, want %v", tt.a, tt.b, got, tt.wantGt)
			}
		})
	}
}

// =============================================================================
// Hex Conversion Tests
// =============================================================================

func TestHexToBigInt(t *testing.T) {
	tests := []struct {
		name string
		hex  string
		want string
	}{
		{"zero", "0x0", "0"},
		{"one", "0x1", "1"},
		{"ten", "0xa", "10"},
		{"255", "0xff", "255"},
		{"256", "0x100", "256"},
		{"without prefix", "ff", "255"},
		{"1 ETH in wei", "0xde0b6b3a7640000", "1000000000000000000"},
		{"empty with prefix", "0x", "0"},
		{"uppercase hex", "0xFF", "255"},
		{"mixed case", "0xAbCdEf", "11259375"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hexToBigInt(tt.hex)
			if got.String() != tt.want {
				t.Errorf("hexToBigInt(%q) = %s, want %s", tt.hex, got.String(), tt.want)
			}
		})
	}
}

// =============================================================================
// Base64 Encoding Tests
// =============================================================================

func TestDecodeBase64(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"standard encoding", base64.StdEncoding.EncodeToString([]byte("hello")), "hello", false},
		{"url safe encoding", base64.URLEncoding.EncodeToString([]byte("hello")), "hello", false},
		{"empty string", "", "", false},
		{"invalid base64", "not-valid-base64!!!", "", true},
		{"json payload", base64.StdEncoding.EncodeToString([]byte(`{"key":"value"}`)), `{"key":"value"}`, false},
		{"with special chars", base64.URLEncoding.EncodeToString([]byte("hello+world/test")), "hello+world/test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBase64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("decodeBase64(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && string(got) != tt.want {
				t.Errorf("decodeBase64(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Nonce Generation Tests
// =============================================================================

func TestGenerateNonce(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		nonce, err := generateNonce()
		if err != nil {
			t.Fatalf("generateNonce() error: %v", err)
		}
		if !strings.HasPrefix(nonce, "0x") {
			t.Errorf("generateNonce() should start with 0x, got %q", nonce)
		}
		if len(nonce) != 66 { // 0x + 64 hex chars
			t.Errorf("generateNonce() should be 66 chars, got %d", len(nonce))
		}
	})

	t.Run("uniqueness", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			nonce, err := generateNonce()
			if err != nil {
				t.Fatalf("generateNonce() error: %v", err)
			}
			if seen[nonce] {
				t.Errorf("generateNonce() generated duplicate: %s", nonce)
			}
			seen[nonce] = true
		}
	})

	t.Run("valid hex", func(t *testing.T) {
		nonce, err := generateNonce()
		if err != nil {
			t.Fatalf("generateNonce() error: %v", err)
		}
		hexPart := strings.TrimPrefix(nonce, "0x")
		for _, c := range hexPart {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("generateNonce() contains invalid hex char: %c", c)
			}
		}
	})
}

// =============================================================================
// Keccak256 Tests
// =============================================================================

func TestKeccak256(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"hello", []byte("hello")},
		{"binary data", []byte{0x00, 0x01, 0x02, 0xff}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := keccak256(tt.input)
			if len(hash) != 32 {
				t.Errorf("keccak256 should return 32 bytes, got %d", len(hash))
			}
		})
	}

	t.Run("deterministic", func(t *testing.T) {
		input := []byte("test data")
		hash1 := keccak256(input)
		hash2 := keccak256(input)
		if string(hash1) != string(hash2) {
			t.Errorf("keccak256 should be deterministic")
		}
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		hash1 := keccak256([]byte("input1"))
		hash2 := keccak256([]byte("input2"))
		if string(hash1) == string(hash2) {
			t.Errorf("different inputs should produce different hashes")
		}
	})
}

// =============================================================================
// Payment Requirement Selection Tests
// =============================================================================

func TestSelectPaymentRequirement(t *testing.T) {
	baseReqs := []PaymentRequirement{
		{Scheme: "exact", Network: "eip155:8453", MaxAmountRequired: "0.01"},
		{Scheme: "exact", Network: "eip155:84532", MaxAmountRequired: "0.02"},
		{Scheme: "exact", Network: "solana:mainnet", MaxAmountRequired: "0.03"},
	}

	tests := []struct {
		name      string
		reqs      []PaymentRequirement
		preferred string
		wantNet   string
		wantNil   bool
	}{
		{"match base mainnet", baseReqs, NetworkBaseMainnet, "eip155:8453", false},
		{"match base sepolia", baseReqs, NetworkBaseSepolia, "eip155:84532", false},
		{"fallback to EVM", baseReqs, "unknown", "eip155:8453", false},
		{"empty requirements", nil, NetworkBaseMainnet, "", true},
		{"empty requirements slice", []PaymentRequirement{}, NetworkBaseMainnet, "", true},
		{"non-EVM only", []PaymentRequirement{{Network: "solana:mainnet"}}, "unknown", "solana:mainnet", false},
		{"direct CAIP2 match", baseReqs, "eip155:8453", "eip155:8453", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectPaymentRequirement(tt.reqs, tt.preferred)
			if tt.wantNil {
				if got != nil {
					t.Errorf("selectPaymentRequirement() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Errorf("selectPaymentRequirement() = nil, want network %s", tt.wantNet)
				return
			}
			if got.Network != tt.wantNet {
				t.Errorf("selectPaymentRequirement() network = %s, want %s", got.Network, tt.wantNet)
			}
		})
	}
}

// =============================================================================
// Crypto Function Tests
// =============================================================================

func TestGenerateKey(t *testing.T) {
	_, err := generateKey()
	if err == nil {
		t.Error("generateKey() should return error (not implemented)")
	}
	if !strings.Contains(err.Error(), "crypto library") {
		t.Errorf("generateKey() error should mention crypto library, got: %v", err)
	}
}

func TestHexToECDSA(t *testing.T) {
	tests := []struct {
		name    string
		hexkey  string
		wantErr bool
	}{
		{"valid hex", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", true}, // returns error but parses hex
		{"invalid hex", "not-hex", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := hexToECDSA(tt.hexkey)
			if (err != nil) != tt.wantErr {
				t.Errorf("hexToECDSA(%q) error = %v, wantErr %v", tt.hexkey, err, tt.wantErr)
			}
		})
	}
}

func TestPubkeyToAddress(t *testing.T) {
	// The function returns a placeholder address
	addr := pubkeyToAddress(nil)
	if addr != "0x0000000000000000000000000000000000000000" {
		t.Errorf("pubkeyToAddress() = %s, want placeholder address", addr)
	}
}

// =============================================================================
// CDP Wallet Tests
// =============================================================================

func TestInitCDPWallet(t *testing.T) {
	ctx := context.Background()

	t.Run("missing credentials", func(t *testing.T) {
		os.Unsetenv("CDP_API_KEY_ID")
		os.Unsetenv("CDP_API_KEY_SECRET")
		_, err := initCDPWallet(ctx, NetworkBaseSepolia)
		if err == nil {
			t.Error("initCDPWallet() should fail without credentials")
		}
		if !strings.Contains(err.Error(), "requires") {
			t.Errorf("error should mention 'requires', got: %v", err)
		}
	})

	t.Run("with credentials", func(t *testing.T) {
		os.Setenv("CDP_API_KEY_ID", "test-key-id")
		os.Setenv("CDP_API_KEY_SECRET", "test-secret")
		defer os.Unsetenv("CDP_API_KEY_ID")
		defer os.Unsetenv("CDP_API_KEY_SECRET")

		_, err := initCDPWallet(ctx, NetworkBaseSepolia)
		if err == nil {
			t.Error("initCDPWallet() should fail (not implemented)")
		}
		if !strings.Contains(err.Error(), "pending") {
			t.Errorf("error should mention 'pending', got: %v", err)
		}
	})
}

// =============================================================================
// Local Wallet Tests
// =============================================================================

func TestInitLocalWallet(t *testing.T) {
	ctx := context.Background()

	t.Run("without key path - generates key", func(t *testing.T) {
		_, err := initLocalWallet(ctx, nil, NetworkBaseSepolia, "")
		if err == nil {
			t.Error("initLocalWallet() should fail when generating key (not implemented)")
		}
	})

	t.Run("with nonexistent key path", func(t *testing.T) {
		_, err := initLocalWallet(ctx, nil, NetworkBaseSepolia, "/nonexistent/path/key.hex")
		if err == nil {
			t.Error("initLocalWallet() should fail with nonexistent path")
		}
	})

	t.Run("with invalid key file", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "invalid-key-*.hex")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())
		_, _ = tmpFile.WriteString("not-a-valid-hex-key")
		tmpFile.Close()

		_, err = initLocalWallet(ctx, nil, NetworkBaseSepolia, tmpFile.Name())
		if err == nil {
			t.Error("initLocalWallet() should fail with invalid key")
		}
	})

	t.Run("with valid hex key file", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "valid-key-*.hex")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())
		// Valid 32-byte hex key
		_, _ = tmpFile.WriteString("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
		tmpFile.Close()

		_, err = initLocalWallet(ctx, nil, NetworkBaseSepolia, tmpFile.Name())
		// Will fail because hexToECDSA returns error (crypto library not available)
		if err == nil {
			t.Error("initLocalWallet() should fail (crypto library not available)")
		}
	})
}

// =============================================================================
// Payment Execution Tests
// =============================================================================

func TestExecuteCDPPayment(t *testing.T) {
	ctx := context.Background()
	wallet := &WalletConfig{
		Type:    WalletTypeCDP,
		Network: NetworkBaseSepolia,
		Address: "0x1234",
	}

	_, err := executeCDPPayment(ctx, wallet, "0x5678", "1.00", "USDC")
	if err == nil {
		t.Error("executeCDPPayment() should fail (not implemented)")
	}
	if !strings.Contains(err.Error(), "coinbase-sdk-go") {
		t.Errorf("error should mention coinbase-sdk-go, got: %v", err)
	}
}

func TestExecuteLocalPayment(t *testing.T) {
	ctx := context.Background()

	t.Run("without key path", func(t *testing.T) {
		wallet := &WalletConfig{
			Type:    WalletTypeLocal,
			Network: NetworkBaseSepolia,
			Address: "0x1234",
			KeyPath: "",
		}
		_, err := executeLocalPayment(ctx, wallet, "0x5678", "1.00", "USDC")
		if err == nil {
			t.Error("executeLocalPayment() should fail without key_path")
		}
		if !strings.Contains(err.Error(), "key_path") {
			t.Errorf("error should mention key_path, got: %v", err)
		}
	})

	t.Run("with key path", func(t *testing.T) {
		wallet := &WalletConfig{
			Type:    WalletTypeLocal,
			Network: NetworkBaseSepolia,
			Address: "0x1234",
			KeyPath: "/some/path/key.hex",
		}
		_, err := executeLocalPayment(ctx, wallet, "0x5678", "1.00", "USDC")
		if err == nil {
			t.Error("executeLocalPayment() should fail (not implemented)")
		}
	})
}

// =============================================================================
// Payment Payload Tests
// =============================================================================

func TestCreatePaymentPayload(t *testing.T) {
	ctx := context.Background()
	wallet := &WalletConfig{
		Type:    WalletTypeLocal,
		Network: NetworkBaseSepolia,
		Address: "0x1234567890123456789012345678901234567890",
	}
	req := &PaymentRequirement{
		Scheme:            "exact",
		Network:           "eip155:84532",
		MaxAmountRequired: "0.01",
		PayTo:             "0x0987654321098765432109876543210987654321",
		MaxTimeoutSeconds: 300,
	}

	payload, err := createPaymentPayload(ctx, wallet, req)
	if err != nil {
		t.Errorf("createPaymentPayload() error = %v", err)
		return
	}

	// Decode and verify structure
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Errorf("payload should be valid base64: %v", err)
		return
	}

	var parsed map[string]any
	if err := json.Unmarshal(decoded, &parsed); err != nil {
		t.Errorf("payload should be valid JSON: %v", err)
		return
	}

	if parsed["x402Version"] != float64(1) {
		t.Errorf("x402Version = %v, want 1", parsed["x402Version"])
	}
	if parsed["scheme"] != "exact" {
		t.Errorf("scheme = %v, want exact", parsed["scheme"])
	}
	if parsed["network"] != "eip155:84532" {
		t.Errorf("network = %v, want eip155:84532", parsed["network"])
	}
}

// =============================================================================
// Wallet Config Persistence Tests
// =============================================================================

func TestWalletConfigPath(t *testing.T) {
	t.Run("with FOXCTL_HOME", func(t *testing.T) {
		os.Setenv("FOXCTL_HOME", "/custom/path")
		defer os.Unsetenv("FOXCTL_HOME")

		path := walletConfigPath(nil)
		if path != "/custom/path/x402_wallet.json" {
			t.Errorf("walletConfigPath() = %s, want /custom/path/x402_wallet.json", path)
		}
	})

	t.Run("without FOXCTL_HOME", func(t *testing.T) {
		os.Unsetenv("FOXCTL_HOME")
		home := os.Getenv("HOME")

		path := walletConfigPath(nil)
		expected := filepath.Join(home, ".foxctl", "x402_wallet.json")
		if path != expected {
			t.Errorf("walletConfigPath() = %s, want %s", path, expected)
		}
	})
}

func TestSaveAndLoadWalletConfig(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "x402-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("FOXCTL_HOME", tmpDir)
	defer os.Unsetenv("FOXCTL_HOME")

	wallet := &WalletInfo{
		Address: "0x1234567890123456789012345678901234567890",
		Network: NetworkBaseSepolia,
		Type:    WalletTypeLocal,
	}

	t.Run("save config", func(t *testing.T) {
		err := saveWalletConfig(nil, wallet, "/path/to/key.hex")
		if err != nil {
			t.Errorf("saveWalletConfig() error = %v", err)
		}

		// Verify file exists
		configPath := filepath.Join(tmpDir, "x402_wallet.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Error("config file was not created")
		}
	})

	t.Run("load config", func(t *testing.T) {
		cfg, err := loadWalletConfig(nil)
		if err != nil {
			t.Errorf("loadWalletConfig() error = %v", err)
			return
		}

		if cfg.Address != wallet.Address {
			t.Errorf("Address = %s, want %s", cfg.Address, wallet.Address)
		}
		if cfg.Network != wallet.Network {
			t.Errorf("Network = %s, want %s", cfg.Network, wallet.Network)
		}
		if cfg.Type != wallet.Type {
			t.Errorf("Type = %s, want %s", cfg.Type, wallet.Type)
		}
		if cfg.KeyPath != "/path/to/key.hex" {
			t.Errorf("KeyPath = %s, want /path/to/key.hex", cfg.KeyPath)
		}
	})
}

func TestLoadWalletConfigFromEnv(t *testing.T) {
	// Clear file-based config
	tmpDir, _ := os.MkdirTemp("", "x402-test-*")
	defer os.RemoveAll(tmpDir)
	os.Setenv("FOXCTL_HOME", tmpDir)
	defer os.Unsetenv("FOXCTL_HOME")

	t.Run("with env vars", func(t *testing.T) {
		os.Setenv("X402_WALLET_ADDRESS", "0xenvaddress")
		os.Setenv("X402_NETWORK", NetworkBaseMainnet)
		defer os.Unsetenv("X402_WALLET_ADDRESS")
		defer os.Unsetenv("X402_NETWORK")

		cfg, err := loadWalletConfig(nil)
		if err != nil {
			t.Errorf("loadWalletConfig() error = %v", err)
			return
		}

		if cfg.Address != "0xenvaddress" {
			t.Errorf("Address = %s, want 0xenvaddress", cfg.Address)
		}
		if cfg.Network != NetworkBaseMainnet {
			t.Errorf("Network = %s, want %s", cfg.Network, NetworkBaseMainnet)
		}
		if cfg.Type != "env" {
			t.Errorf("Type = %s, want env", cfg.Type)
		}
	})

	t.Run("env address without network", func(t *testing.T) {
		os.Setenv("X402_WALLET_ADDRESS", "0xenvaddress")
		os.Unsetenv("X402_NETWORK")
		defer os.Unsetenv("X402_WALLET_ADDRESS")

		cfg, err := loadWalletConfig(nil)
		if err != nil {
			t.Errorf("loadWalletConfig() error = %v", err)
			return
		}

		if cfg.Network != NetworkBaseSepolia {
			t.Errorf("Network = %s, want default %s", cfg.Network, NetworkBaseSepolia)
		}
	})

	t.Run("no config", func(t *testing.T) {
		os.Unsetenv("X402_WALLET_ADDRESS")
		_, err := loadWalletConfig(nil)
		if err == nil {
			t.Error("loadWalletConfig() should fail without config")
		}
	})
}

// =============================================================================
// JSON-RPC Tests
// =============================================================================

func TestRPCCall(t *testing.T) {
	t.Run("successful call", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			if r.Method != "POST" {
				t.Errorf("method = %s, want POST", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %s, want application/json", r.Header.Get("Content-Type"))
			}

			body, _ := io.ReadAll(r.Body)
			var req JSONRPCRequest
			_ = json.Unmarshal(body, &req)

			if req.Method != "eth_blockNumber" {
				t.Errorf("RPC method = %s, want eth_blockNumber", req.Method)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x123"}`))
		})

		withRPCClient(t, handler, func() {
			ctx := context.Background()
			req := JSONRPCRequest{
				JSONRPC: "2.0",
				Method:  "eth_blockNumber",
				Params:  []any{},
				ID:      1,
			}

			result, err := rpcCall(ctx, "http://mock", req)
			if err != nil {
				t.Errorf("rpcCall() error = %v", err)
				return
			}

			var hexResult string
			_ = json.Unmarshal(result, &hexResult)
			if hexResult != "0x123" {
				t.Errorf("result = %s, want 0x123", hexResult)
			}
		})
	})

	t.Run("RPC error", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`))
		})

		withRPCClient(t, handler, func() {
			ctx := context.Background()
			req := JSONRPCRequest{JSONRPC: "2.0", Method: "test", ID: 1}

			_, err := rpcCall(ctx, "http://mock", req)
			if err == nil {
				t.Error("rpcCall() should return error on RPC error")
			}
			if !strings.Contains(err.Error(), "Invalid Request") {
				t.Errorf("error should contain 'Invalid Request', got: %v", err)
			}
		})
	})

	t.Run("HTTP error", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		withRPCClient(t, handler, func() {
			ctx := context.Background()
			req := JSONRPCRequest{JSONRPC: "2.0", Method: "test", ID: 1}

			_, err := rpcCall(ctx, "http://mock", req)
			if err == nil {
				t.Error("rpcCall() should return error on invalid JSON response")
			}
		})
	})

	t.Run("connection error", func(t *testing.T) {
		ctx := context.Background()
		req := JSONRPCRequest{JSONRPC: "2.0", Method: "test", ID: 1}

		_, err := rpcCall(ctx, "http://localhost:99999", req)
		if err == nil {
			t.Error("rpcCall() should return error on connection failure")
		}
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		})

		withRPCClient(t, handler, func() {
			ctx := context.Background()
			req := JSONRPCRequest{JSONRPC: "2.0", Method: "test", ID: 1}

			_, err := rpcCall(ctx, "http://mock", req)
			if err == nil {
				t.Error("rpcCall() should return error on invalid JSON")
			}
		})
	})
}

func TestRPCGetBalance(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req JSONRPCRequest
		_ = json.Unmarshal(body, &req)

		if req.Method != "eth_getBalance" {
			t.Errorf("method = %s, want eth_getBalance", req.Method)
		}

		// Return 1 ETH in wei
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0xde0b6b3a7640000"}`))
	})

	withRPCClient(t, handler, func() {
		ctx := context.Background()
		balance, err := rpcGetBalance(ctx, "http://mock", "0x1234")
		if err != nil {
			t.Errorf("rpcGetBalance() error = %v", err)
			return
		}

		if balance != "1.000000" {
			t.Errorf("balance = %s, want 1.000000", balance)
		}
	})
}

func TestRPCGetERC20Balance(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req JSONRPCRequest
		_ = json.Unmarshal(body, &req)

		if req.Method != "eth_call" {
			t.Errorf("method = %s, want eth_call", req.Method)
		}

		// Return 100 USDC (100 * 10^6)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x5f5e100"}`))
	})

	withRPCClient(t, handler, func() {
		ctx := context.Background()
		balance, err := rpcGetERC20Balance(ctx, "http://mock", "0x1234", "0xUSDC")
		if err != nil {
			t.Errorf("rpcGetERC20Balance() error = %v", err)
			return
		}

		if balance != "100.00" {
			t.Errorf("balance = %s, want 100.00", balance)
		}
	})
}

func TestGetBalances(t *testing.T) {
	t.Run("unknown network", func(t *testing.T) {
		ctx := context.Background()
		_, err := getBalances(ctx, "0x1234", "unknown-network")
		if err == nil {
			t.Error("getBalances() should fail for unknown network")
		}
	})

	t.Run("with mock RPC", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var req JSONRPCRequest
			_ = json.Unmarshal(body, &req)

			switch req.Method {
			case "eth_getBalance":
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0xde0b6b3a7640000"}`))
			case "eth_call":
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x5f5e100"}`))
			}
		})

		withRPCClient(t, handler, func() {
			ctx := context.Background()
			balances, err := getBalances(ctx, "0x1234", NetworkBaseSepolia)
			if err != nil {
				t.Errorf("getBalances() error = %v", err)
				return
			}
			if balances["ETH"] != "1.000000" {
				t.Errorf("ETH balance = %s, want 1.000000", balances["ETH"])
			}
			if balances["USDC"] != "100.00" {
				t.Errorf("USDC balance = %s, want 100.00", balances["USDC"])
			}
		})
	})
}

// =============================================================================
// HTTP Fetch Tests
// =============================================================================

func TestHandleFetchValidation(t *testing.T) {
	t.Run("missing URL", func(t *testing.T) {
		ctx := context.Background()
		in := Input{Operation: OpFetch, URL: ""}
		err := handleFetch(ctx, nil, in)
		if err == nil {
			t.Error("handleFetch() should fail without URL")
		}
		if !strings.Contains(err.Error(), "url is required") {
			t.Errorf("error should mention url, got: %v", err)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		ctx := context.Background()
		in := Input{Operation: OpFetch, URL: "://invalid", Method: "GET"}
		err := handleFetch(ctx, nil, in)
		if err == nil {
			t.Error("handleFetch() should fail with invalid URL")
		}
	})
}

func TestHandlePayValidation(t *testing.T) {
	t.Run("missing to address", func(t *testing.T) {
		ctx := context.Background()
		in := Input{Operation: OpPay, To: "", Amount: "1.00"}
		err := handlePay(ctx, nil, in)
		if err == nil {
			t.Error("handlePay() should fail without to address")
		}
		if !strings.Contains(err.Error(), "to is required") {
			t.Errorf("error should mention to is required, got: %v", err)
		}
	})

	t.Run("missing amount", func(t *testing.T) {
		ctx := context.Background()
		in := Input{Operation: OpPay, To: "0x1234", Amount: ""}
		err := handlePay(ctx, nil, in)
		if err == nil {
			t.Error("handlePay() should fail without amount")
		}
		if !strings.Contains(err.Error(), "amount") {
			t.Errorf("error should mention amount, got: %v", err)
		}
	})
}

// =============================================================================
// JSON Serialization Tests
// =============================================================================

func TestPaymentRequirementJSON(t *testing.T) {
	req := PaymentRequirement{
		Scheme:            "exact",
		Network:           "eip155:8453",
		MaxAmountRequired: "0.01",
		Resource:          "https://api.example.com/data",
		Description:       "API access",
		MimeType:          "application/json",
		PayTo:             "0x1234567890123456789012345678901234567890",
		MaxTimeoutSeconds: 300,
		Asset:             "USDC",
		Extra:             map[string]any{"custom": "field"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded PaymentRequirement
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Scheme != req.Scheme {
		t.Errorf("Scheme mismatch")
	}
	if decoded.Network != req.Network {
		t.Errorf("Network mismatch")
	}
	if decoded.MaxAmountRequired != req.MaxAmountRequired {
		t.Errorf("MaxAmountRequired mismatch")
	}
	if decoded.PayTo != req.PayTo {
		t.Errorf("PayTo mismatch")
	}
	if decoded.MaxTimeoutSeconds != req.MaxTimeoutSeconds {
		t.Errorf("MaxTimeoutSeconds mismatch")
	}
}

func TestWalletInfoJSON(t *testing.T) {
	wallet := WalletInfo{
		Address: "0x1234567890123456789012345678901234567890",
		Network: NetworkBaseSepolia,
		Type:    WalletTypeLocal,
		Balances: map[string]string{
			"ETH":  "0.1",
			"USDC": "100.00",
		},
		CAIP2: "eip155:84532",
	}

	data, err := json.Marshal(wallet)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded WalletInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Address != wallet.Address {
		t.Errorf("Address mismatch")
	}
	if decoded.Network != wallet.Network {
		t.Errorf("Network mismatch")
	}
	if decoded.Type != wallet.Type {
		t.Errorf("Type mismatch")
	}
	if decoded.Balances["USDC"] != "100.00" {
		t.Errorf("USDC balance mismatch")
	}
	if decoded.CAIP2 != wallet.CAIP2 {
		t.Errorf("CAIP2 mismatch")
	}
}

func TestPaymentInfoJSON(t *testing.T) {
	payment := PaymentInfo{
		TxHash:    "0xabcdef",
		From:      "0x1234",
		To:        "0x5678",
		Amount:    "1.00",
		Asset:     "USDC",
		Network:   NetworkBaseSepolia,
		Status:    "submitted",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(payment)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded PaymentInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.TxHash != payment.TxHash {
		t.Errorf("TxHash mismatch")
	}
	if decoded.Status != payment.Status {
		t.Errorf("Status mismatch")
	}
}

func TestWalletConfigJSON(t *testing.T) {
	cfg := WalletConfig{
		Type:      WalletTypeLocal,
		Network:   NetworkBaseSepolia,
		Address:   "0x1234",
		KeyPath:   "/path/to/key",
		CDPKeyID:  "",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded WalletConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != cfg.Type {
		t.Errorf("Type mismatch")
	}
	if decoded.KeyPath != cfg.KeyPath {
		t.Errorf("KeyPath mismatch")
	}
}

func TestOutputJSON(t *testing.T) {
	output := Output{
		Operation: OpWalletStatus,
		Wallet: &WalletInfo{
			Address: "0x1234",
			Network: NetworkBaseSepolia,
		},
		Error: "",
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Output
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Operation != output.Operation {
		t.Errorf("Operation mismatch")
	}
	if decoded.Wallet == nil {
		t.Error("Wallet should not be nil")
	}
}

func TestHTTPResponseJSON(t *testing.T) {
	resp := HTTPResponse{
		StatusCode:    200,
		Headers:       map[string]string{"Content-Type": "application/json"},
		Body:          map[string]any{"data": "value"},
		PaymentMade:   true,
		PaymentAmount: "0.01",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded HTTPResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.StatusCode != resp.StatusCode {
		t.Errorf("StatusCode mismatch")
	}
	if decoded.PaymentMade != resp.PaymentMade {
		t.Errorf("PaymentMade mismatch")
	}
}

func TestInputJSON(t *testing.T) {
	input := Input{
		Operation:  OpFetch,
		WalletType: WalletTypeCDP,
		Network:    NetworkBaseSepolia,
		URL:        "https://example.com",
		Method:     "GET",
		Headers:    map[string]string{"Authorization": "Bearer token"},
		Body:       `{"key": "value"}`,
		MaxPayment: "1.00",
		AutoPay:    true,
		To:         "0x1234",
		Amount:     "0.50",
		Asset:      "USDC",
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Input
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Operation != input.Operation {
		t.Errorf("Operation mismatch")
	}
	if decoded.AutoPay != input.AutoPay {
		t.Errorf("AutoPay mismatch")
	}
}

// =============================================================================
// Constants Tests
// =============================================================================

func TestConstants(t *testing.T) {
	// Verify command name
	if commandName != "x402/payment" {
		t.Errorf("commandName = %s, want x402/payment", commandName)
	}

	// Verify operations
	if OpWalletInit != "wallet/init" {
		t.Errorf("OpWalletInit = %s, want wallet/init", OpWalletInit)
	}
	if OpWalletStatus != "wallet/status" {
		t.Errorf("OpWalletStatus = %s, want wallet/status", OpWalletStatus)
	}
	if OpFetch != "fetch" {
		t.Errorf("OpFetch = %s, want fetch", OpFetch)
	}
	if OpPay != "pay" {
		t.Errorf("OpPay = %s, want pay", OpPay)
	}

	// Verify wallet types
	if WalletTypeCDP != "cdp" {
		t.Errorf("WalletTypeCDP = %s, want cdp", WalletTypeCDP)
	}
	if WalletTypeLocal != "local" {
		t.Errorf("WalletTypeLocal = %s, want local", WalletTypeLocal)
	}

	// Verify networks
	if NetworkBaseMainnet != "base-mainnet" {
		t.Errorf("NetworkBaseMainnet = %s, want base-mainnet", NetworkBaseMainnet)
	}
	if NetworkBaseSepolia != "base-sepolia" {
		t.Errorf("NetworkBaseSepolia = %s, want base-sepolia", NetworkBaseSepolia)
	}

	// Verify USDC addresses are valid checksummed addresses
	if !strings.HasPrefix(USDCBaseMainnet, "0x") || len(USDCBaseMainnet) != 42 {
		t.Errorf("USDCBaseMainnet invalid format: %s", USDCBaseMainnet)
	}
	if !strings.HasPrefix(USDCBaseSepolia, "0x") || len(USDCBaseSepolia) != 42 {
		t.Errorf("USDCBaseSepolia invalid format: %s", USDCBaseSepolia)
	}

	// Verify RPC URLs
	if !strings.HasPrefix(RPCBaseMainnet, "https://") {
		t.Errorf("RPCBaseMainnet should use HTTPS: %s", RPCBaseMainnet)
	}
	if !strings.HasPrefix(RPCBaseSepolia, "https://") {
		t.Errorf("RPCBaseSepolia should use HTTPS: %s", RPCBaseSepolia)
	}
}

// =============================================================================
// Run Function Tests
// =============================================================================

func TestRunOperationDispatch(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown operation", func(t *testing.T) {
		in := Input{Operation: "unknown"}
		err := run(ctx, nil, in)
		if err == nil {
			t.Error("run() should fail for unknown operation")
		}
		if !strings.Contains(err.Error(), "invalid operation") {
			t.Errorf("error should mention invalid operation, got: %v", err)
		}
	})

	t.Run("empty operation", func(t *testing.T) {
		in := Input{Operation: ""}
		err := run(ctx, nil, in)
		if err == nil {
			t.Error("run() should fail for empty operation")
		}
	})
}

// =============================================================================
// Handle Wallet Init Tests
// =============================================================================

func TestHandleWalletInitValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown wallet type", func(t *testing.T) {
		in := Input{WalletType: "unknown"}
		err := handleWalletInit(ctx, nil, in)
		if err == nil {
			t.Error("handleWalletInit() should fail for unknown wallet type")
		}
		if !strings.Contains(err.Error(), "unknown wallet type") {
			t.Errorf("error should mention unknown wallet type, got: %v", err)
		}
	})
}

// =============================================================================
// Handle Wallet Status Tests
// =============================================================================

func TestHandleWalletStatusValidation(t *testing.T) {
	ctx := context.Background()

	// Set up empty config directory
	tmpDir, _ := os.MkdirTemp("", "x402-test-*")
	defer os.RemoveAll(tmpDir)
	os.Setenv("FOXCTL_HOME", tmpDir)
	defer os.Unsetenv("FOXCTL_HOME")
	os.Unsetenv("X402_WALLET_ADDRESS")

	t.Run("no wallet configured", func(t *testing.T) {
		in := Input{Operation: OpWalletStatus, Address: ""}
		err := handleWalletStatus(ctx, nil, in)
		if err == nil {
			t.Error("handleWalletStatus() should fail without wallet")
		}
	})
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkHexToBigInt(b *testing.B) {
	hex := "0xde0b6b3a7640000" // 1 ETH in wei
	for i := 0; i < b.N; i++ {
		hexToBigInt(hex)
	}
}

func BenchmarkParseAmount(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = parseAmount("123.456789")
	}
}

func BenchmarkSelectPaymentRequirement(b *testing.B) {
	reqs := []PaymentRequirement{
		{Network: "eip155:1"},
		{Network: "eip155:8453"},
		{Network: "eip155:84532"},
		{Network: "solana:mainnet"},
	}
	for i := 0; i < b.N; i++ {
		selectPaymentRequirement(reqs, NetworkBaseSepolia)
	}
}

func BenchmarkKeccak256(b *testing.B) {
	data := []byte("test data for hashing benchmark")
	for i := 0; i < b.N; i++ {
		keccak256(data)
	}
}

func BenchmarkGenerateNonce(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = generateNonce()
	}
}

func BenchmarkDecodeBase64(b *testing.B) {
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"key":"value","nested":{"a":1,"b":2}}`))
	for i := 0; i < b.N; i++ {
		_, _ = decodeBase64(encoded)
	}
}

func BenchmarkNetworkToCAIP2(b *testing.B) {
	for i := 0; i < b.N; i++ {
		networkToCAIP2(NetworkBaseSepolia)
	}
}

func BenchmarkCreatePaymentPayload(b *testing.B) {
	ctx := context.Background()
	wallet := &WalletConfig{Address: "0x1234"}
	req := &PaymentRequirement{
		Scheme:            "exact",
		Network:           "eip155:84532",
		MaxAmountRequired: "0.01",
		PayTo:             "0x5678",
		MaxTimeoutSeconds: 300,
	}
	for i := 0; i < b.N; i++ {
		_, _ = createPaymentPayload(ctx, wallet, req)
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestEdgeCases(t *testing.T) {
	t.Run("hexToBigInt empty string", func(t *testing.T) {
		result := hexToBigInt("")
		if result.String() != "0" {
			t.Errorf("hexToBigInt('') = %s, want 0", result.String())
		}
	})

	t.Run("hexToBigInt only prefix", func(t *testing.T) {
		result := hexToBigInt("0x")
		if result.String() != "0" {
			t.Errorf("hexToBigInt('0x') = %s, want 0", result.String())
		}
	})

	t.Run("parseAmount zero", func(t *testing.T) {
		result, err := parseAmount("0")
		if err != nil {
			t.Errorf("parseAmount('0') error = %v", err)
		}
		if result.Cmp(big.NewFloat(0)) != 0 {
			t.Errorf("parseAmount('0') = %v, want 0", result)
		}
	})

	t.Run("selectPaymentRequirement single non-EVM", func(t *testing.T) {
		reqs := []PaymentRequirement{{Network: "bitcoin:mainnet"}}
		result := selectPaymentRequirement(reqs, NetworkBaseSepolia)
		if result == nil || result.Network != "bitcoin:mainnet" {
			t.Errorf("should return single non-EVM requirement")
		}
	})
}

// =============================================================================
// Integration-Style Tests (with HTTP test servers)
// =============================================================================

func TestFetchWithTestServer(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Custom") != "header" {
				t.Errorf("custom header not received")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})

		// We can't fully test handleFetch without runner.RunnerContext,
		// but we can test the HTTP interaction patterns
		ctx := context.Background()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://mock", nil)
		req.Header.Set("X-Custom", "header")

		client := &http.Client{
			Timeout:   5 * time.Second,
			Transport: &handlerTransport{handler: handler},
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("request error = %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("402 response", func(t *testing.T) {
		requirements := []PaymentRequirement{{
			Scheme:            "exact",
			Network:           "eip155:84532",
			MaxAmountRequired: "0.01",
			PayTo:             "0x1234",
		}}
		reqJSON, _ := json.Marshal(requirements)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Payment-Required", string(reqJSON))
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"error":"payment required"}`))
		})

		ctx := context.Background()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://mock", nil)

		client := &http.Client{
			Timeout:   5 * time.Second,
			Transport: &handlerTransport{handler: handler},
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("request error = %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusPaymentRequired {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusPaymentRequired)
		}

		paymentHeader := resp.Header.Get("X-Payment-Required")
		if paymentHeader == "" {
			t.Error("X-Payment-Required header missing")
		}

		var parsedReqs []PaymentRequirement
		_ = json.Unmarshal([]byte(paymentHeader), &parsedReqs)
		if len(parsedReqs) != 1 {
			t.Errorf("parsed requirements count = %d, want 1", len(parsedReqs))
		}
	})
}

// =============================================================================
// Error Message Tests
// =============================================================================

func TestErrorMessages(t *testing.T) {
	ctx := context.Background()

	errors := []struct {
		name     string
		fn       func() error
		contains string
	}{
		{
			name: "run unknown operation",
			fn: func() error {
				return run(ctx, nil, Input{Operation: "bad"})
			},
			contains: "invalid operation",
		},
		{
			name: "handleWalletInit unknown type",
			fn: func() error {
				return handleWalletInit(ctx, nil, Input{WalletType: "bad"})
			},
			contains: "unknown wallet type",
		},
		{
			name: "handleFetch no URL",
			fn: func() error {
				return handleFetch(ctx, nil, Input{})
			},
			contains: "url is required",
		},
		{
			name: "handlePay no to",
			fn: func() error {
				return handlePay(ctx, nil, Input{Amount: "1"})
			},
			contains: "to is required",
		},
		{
			name: "handlePay no amount",
			fn: func() error {
				return handlePay(ctx, nil, Input{To: "0x1234"})
			},
			contains: "amount",
		},
		{
			name: "executeLocalPayment no key",
			fn: func() error {
				_, err := executeLocalPayment(ctx, &WalletConfig{}, "0x1234", "1", "USDC")
				return err
			},
			contains: "key_path",
		},
	}

	for _, tc := range errors {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Error("expected error")
				return
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("error %q should contain %q", err.Error(), tc.contains)
			}
		})
	}
}

type handlerTransport struct {
	handler http.Handler
}

func (t *handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return recorder.Result(), nil
}

func withRPCClient(t *testing.T, handler http.Handler, fn func()) {
	t.Helper()
	prev := rpcHTTPClient
	rpcHTTPClient = &http.Client{
		Timeout:   10 * time.Second,
		Transport: &handlerTransport{handler: handler},
	}
	t.Cleanup(func() {
		rpcHTTPClient = prev
	})
	fn()
}

// Prevent unused import errors
var _ = fmt.Sprintf
