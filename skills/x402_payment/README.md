# x402/payment Skill

AI-native HTTP micropayment skill using the x402 protocol.

## Overview

This skill implements the [x402 protocol](https://x402.org) for handling HTTP 402 Payment Required responses with cryptocurrency micropayments. It enables AI agents to pay for API access and content automatically.

## Operations

| Operation | Description |
|-----------|-------------|
| `wallet/init` | Initialize a wallet (CDP managed or local) |
| `wallet/status` | Check wallet balance and status |
| `fetch` | Fetch URL with automatic 402 payment handling |
| `pay` | Execute a direct payment |

## Usage

```bash
# Initialize a CDP wallet (recommended)
foxctl run x402/payment --input '{
  "operation": "wallet/init",
  "wallet_type": "cdp",
  "network": "base-sepolia"
}'

# Check wallet status
foxctl run x402/payment --input '{
  "operation": "wallet/status"
}'

# Fetch a 402-protected resource (auto-pay)
foxctl run x402/payment --input '{
  "operation": "fetch",
  "url": "https://api.example.com/premium/data",
  "max_payment": "0.10",
  "auto_pay": true
}'

# Direct payment
foxctl run x402/payment --input '{
  "operation": "pay",
  "to": "0x...",
  "amount": "0.05",
  "asset": "USDC"
}'
```

## Wallet Types

### CDP (Coinbase Developer Platform) - Recommended

Uses Coinbase's secure infrastructure. Keys are managed in a Trusted Execution Environment (TEE).

**Requirements:**
- `CDP_API_KEY_ID` environment variable
- `CDP_API_KEY_SECRET` environment variable

Get credentials at: https://portal.cdp.coinbase.com/projects/api-keys

### Local Wallet

Self-managed wallet using a local private key file.

**Requirements:**
- Private key file (hex encoded)
- Provide via `key_path` parameter

## Networks

| Network | CAIP-2 | RPC |
|---------|--------|-----|
| `base-mainnet` | `eip155:8453` | mainnet.base.org |
| `base-sepolia` | `eip155:84532` | sepolia.base.org |
| `solana-mainnet` | `solana:*` | (planned) |
| `solana-devnet` | `solana:*` | (planned) |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `CDP_API_KEY_ID` | Coinbase CDP API key ID |
| `CDP_API_KEY_SECRET` | Coinbase CDP API key secret |
| `X402_WALLET_ADDRESS` | Override default wallet address |
| `X402_NETWORK` | Override default network |

## Dependencies

For full functionality, install:

```bash
# For CDP wallet support
go get github.com/coinbase/coinbase-sdk-go

# For local wallet support (key generation/signing)
go get github.com/ethereum/go-ethereum/crypto

# For x402 protocol
go get github.com/coinbase/x402/go
```

## Building

```bash
cd skills/x402_payment
go build -o x402_payment .
```

Or use the Makefile:

```bash
make skills-install
```

## x402 Protocol

The x402 protocol flow:

1. Client requests a resource
2. Server returns `402 Payment Required` with `X-Payment-Required` header
3. Client creates a payment payload (ERC-3009 authorization for EVM)
4. Client retries request with `X-Payment` header
5. Server verifies payment and returns resource

## References

- [x402 Protocol Spec](https://x402.org)
- [x402 Go SDK](https://github.com/coinbase/x402/tree/main/go)
- [Coinbase CDP SDK](https://github.com/coinbase/coinbase-sdk-go)
- [ERC-3009: Transfer With Authorization](https://eips.ethereum.org/EIPS/eip-3009)
