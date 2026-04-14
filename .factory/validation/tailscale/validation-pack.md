# External Tailnet Validation Pack

Concrete validation procedure for deferred gateway assertions VAL-GW-022, VAL-GW-023, VAL-GW-024.
These assertions require a real Tailscale tailnet and cannot be validated in localhost dev mode.

---

## Prerequisites

| Requirement | Details |
|-------------|---------|
| Tailscale account | Active tailnet with at least one other device |
| TS_AUTHKEY | Tailscale auth key with `--ephemeral` flag. Generate: `tailscale authkeys create --ephemeral` |
| Another tailnet device | Laptop/phone on same tailnet for cross-device testing |
| `curl` with TLS | Standard curl (any version with HTTPS support) |
| Built binary | `CGO_ENABLED=0 go build -o bin/foxctl ./cmd/foxctl/` |

## Environment Setup

```bash
# 1. Set auth key
export TS_AUTHKEY="tskey-auth-..."

# 2. Build binary
make build

# 3. Verify tmux available
tmux -V  # expected: 3.6a or later
```

---

## VAL-GW-022: Gateway uses auto-TLS for HTTPS

**Assertion:** `curl https://<hostname>/healthz` succeeds without `--insecure`.

### Procedure

```bash
# Step 1: Start gateway in Tailscale mode
bin/foxctl gateway --ts-authkey "$TS_AUTHKEY" --hostname test-validation &
GATEWAY_PID=$!
sleep 10  # Wait for tsnet connection + TLS cert provisioning

# Step 2: Extract hostname from gateway logs
# Look for: "tsnet node online" / "dns_name" in stderr output
# Use the full FQDN emitted by tsnet, e.g. test-validation.<tailnet>.ts.net

# Step 3: Test HTTPS healthz WITHOUT --insecure
curl -sf https://test-validation.<your-tailnet>.ts.net/healthz

# Step 4: Verify TLS certificate details
curl -svf https://test-validation.<your-tailnet>.ts.net/healthz 2>&1 | grep -E "subject:|issuer:|SSL connection"

# Cleanup
kill $GATEWAY_PID 2>/dev/null
wait $GATEWAY_PID 2>/dev/null
```

### Expected Results

| Step | Expected |
|------|----------|
| Step 1 | Gateway starts, logs "tsnet connected" with hostname |
| Step 2 | Hostname matches `test-validation.<tailnet>.ts.net` |
| Step 3 | Returns `200` with `{"tsnet":"ok","store":"ok","tmux":"ok"}` — no TLS errors |
| Step 4 | Certificate issued by Tailscale (`issuer: CN=Tailscale Inc.`), valid for the hostname |

### Evidence to Collect

- `curl -svf` output showing TLS handshake and certificate chain
- `healthz` response body (JSON)
- Gateway startup logs showing tsnet hostname

### Pass Criteria

- `curl -sf https://<hostname>/healthz` returns HTTP 200 without `--insecure`
- Certificate is valid (not self-signed, not expired)
- Certificate domain matches the gateway hostname

---

## VAL-GW-023: Gateway accessible via MagicDNS hostname

**Assertion:** Gateway resolves via MagicDNS and responds via the emitted tsnet FQDN. Short-name resolution may work at the DNS layer, but HTTPS certificate validation is only guaranteed against the FQDN emitted by tsnet.

### Procedure

```bash
# Step 1: Start gateway
bin/foxctl gateway --ts-authkey "$TS_AUTHKEY" --hostname test-validation &
GATEWAY_PID=$!
sleep 10

# Step 2: Resolve MagicDNS name and FQDN
nslookup test-validation
nslookup test-validation.<your-tailnet>.ts.net

# Step 3: Access via the FQDN emitted by tsnet
curl -sf https://test-validation.<your-tailnet>.ts.net/healthz

# Step 4: Optional short-name probe (expected to fail TLS hostname verification
# if the certificate only covers the FQDN)
curl -svf https://test-validation/healthz

# Cleanup
kill $GATEWAY_PID 2>/dev/null
wait $GATEWAY_PID 2>/dev/null
```

### Expected Results

| Step | Expected |
|------|----------|
| Step 2 | MagicDNS resolves both `test-validation` and the FQDN to a Tailscale IP (100.x.x.x) |
| Step 3 | Returns 200 via full FQDN |
| Step 4 | Short-name probe either succeeds or fails specifically on certificate hostname validation |

### Evidence to Collect

- `nslookup` or `dig` output showing DNS resolution
- `curl` response from the FQDN
- optional `curl -svf` output from the short name showing whether certificate validation covers it
- `tailscale status` output showing the new node

### Pass Criteria

- MagicDNS resolves the gateway hostname to a Tailscale IP
- HTTPS healthz succeeds via the emitted tsnet FQDN without `--insecure`
- if the short name fails, it fails specifically because the certificate SAN only covers the FQDN

---

## VAL-GW-024: Gateway not accessible outside tailnet

**Assertion:** Gateway is unreachable from non-tailnet interfaces.

### Procedure

```bash
# Step 1: Start gateway in Tailscale mode (NOT --dev)
bin/foxctl gateway --ts-authkey "$TS_AUTHKEY" --hostname test-validation &
GATEWAY_PID=$!
sleep 10

# Step 2: Verify localhost access FAILS (not listening on localhost)
curl -sf http://localhost:8765/healthz 2>&1
# Expected: connection refused

# Step 3: Verify no listener on public interfaces
lsof -i :443 -i :80 -i :8765 | grep foxctl || echo "No public listeners found"

# Step 4: Verify gateway IS accessible from another tailnet device via the FQDN
curl -sf "https://test-validation.<your-tailnet>.ts.net/healthz"

# Step 5: Verify from ANOTHER device on same tailnet
# Run on a different machine:
#   curl -sf https://test-validation.<your-tailnet>.ts.net/healthz
# Expected: 200

# Step 6: Verify from OUTSIDE tailnet (e.g., cellular, different network)
# Run from a device NOT on the tailnet:
#   curl -sf https://test-validation.<tailnet>.ts.net/healthz
# Expected: connection timeout or DNS resolution failure

# Cleanup
kill $GATEWAY_PID 2>/dev/null
wait $GATEWAY_PID 2>/dev/null
```

### Expected Results

| Step | Expected |
|------|----------|
| Step 2 | `curl: (7) Failed to connect to localhost port 8765: Connection refused` |
| Step 3 | No foxctl listeners on public interfaces |
| Step 4 | 200 via Tailscale IP |
| Step 5 | 200 from another tailnet device |
| Step 6 | Connection failure from outside tailnet |

### Evidence to Collect

- `curl` error output from localhost attempt (connection refused)
- `lsof` output showing no public listeners
- Successful `curl` from tailnet device
- Failed `curl` from non-tailnet device (or confirmation that DNS doesn't resolve)

### Pass Criteria

- Gateway does NOT listen on localhost when in Tailscale mode
- Gateway does NOT listen on any public interface
- Gateway IS accessible from other tailnet devices
- Gateway is NOT accessible from outside the tailnet

---

## Quick Smoke Test (All 3 Assertions)

```bash
#!/bin/bash
set -euo pipefail
export TS_AUTHKEY="${TS_AUTHKEY:?Set TS_AUTHKEY}"

# Build
CGO_ENABLED=0 go build -o bin/foxctl ./cmd/foxctl/

# Start gateway
bin/foxctl gateway --ts-authkey "$TS_AUTHKEY" --hostname test-validation &
GPID=$!
sleep 15

echo "=== VAL-GW-022: Auto-TLS ==="
curl -sf https://test-validation.<your-tailnet>.ts.net/healthz && echo " PASS" || echo " FAIL"

echo "=== VAL-GW-023: MagicDNS ==="
curl -sf https://test-validation.<your-tailnet>.ts.net/healthz && echo " PASS" || echo " FAIL"

echo "=== VAL-GW-024: No localhost listener ==="
curl -sf http://localhost:8765/healthz 2>&1 && echo " FAIL (should refuse)" || echo " PASS (connection refused)"

# Cleanup
kill $GPID 2>/dev/null; wait $GPID 2>/dev/null
echo "=== Done ==="
```

## Notes

- Tailscale auto-TLS certificate provisioning can take 5-30 seconds on first connection
- If using an ephemeral key, the node disappears after `kill`; for persistent testing, use a reusable auth key
- The gateway logs MagicDNS hostname on startup — check stderr for `tsnet hostname: test-validation`
- For CI validation, use `--dev` mode as a fallback (validates everything except these 3 assertions)
- Observed on 2026-04-10: second-device HTTPS via `test-validation.tailbf79e9.ts.net` succeeded with HTTP 200 and a valid certificate; the short-name `https://test-validation/healthz` resolved but failed TLS hostname validation because the certificate SAN covered only the FQDN.
