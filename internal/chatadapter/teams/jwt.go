package teams

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const botFrameworkOpenIDConfigURL = "https://login.botframework.com/v1/.well-known/openidconfiguration"

type JWTVerifier interface {
	Verify(ctx context.Context, authHeader string) error
}

type nopJWTVerifier struct{}

func (nopJWTVerifier) Verify(_ context.Context, _ string) error { return nil }

type openIDConfig struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

type jwks struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string   `json:"kty"`
	Use string   `json:"use"`
	Kid string   `json:"kid"`
	X5c []string `json:"x5c"`
	N   string   `json:"n"`
	E   string   `json:"e"`
}

type botClaims struct {
	jwt.RegisteredClaims
	Version         string `json:"ver"`
	AppID           string `json:"appid"`
	AuthorizedParty string `json:"azp"`
	TenantID        string `json:"tid"`
}

type jwtVerifier struct {
	clientID string
	tenantID string

	openIDURL  string
	httpClient *http.Client
	now        func() time.Time

	mu            sync.RWMutex
	issuer        string
	jwksURI       string
	jwksFetchedAt time.Time
	keys          map[string]*rsa.PublicKey
}

func newJWTVerifier(clientID, tenantID string, httpClient *http.Client) *jwtVerifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &jwtVerifier{
		clientID:   strings.TrimSpace(clientID),
		tenantID:   strings.TrimSpace(tenantID),
		openIDURL:  botFrameworkOpenIDConfigURL,
		httpClient: httpClient,
		now:        time.Now,
		keys:       make(map[string]*rsa.PublicKey),
	}
}

func (v *jwtVerifier) Verify(parent context.Context, authHeader string) error {
	if strings.TrimSpace(v.clientID) == "" {
		return fmt.Errorf("teams: jwt verify: missing client id")
	}
	if strings.TrimSpace(v.tenantID) == "" {
		return fmt.Errorf("teams: jwt verify: missing tenant id")
	}

	tokenStr := extractBearerToken(authHeader)
	if tokenStr == "" {
		return fmt.Errorf("teams: missing bearer token")
	}

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	if err := v.ensureOpenID(ctx); err != nil {
		return err
	}
	if err := v.ensureKeys(ctx); err != nil {
		return err
	}

	claims := &botClaims{}
	keyFunc := func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		kid = strings.TrimSpace(kid)
		if kid == "" {
			return nil, fmt.Errorf("teams: jwt: missing kid")
		}

		if key := v.lookupKey(kid); key != nil {
			return key, nil
		}

		// Key rotation: refresh once on miss.
		_ = v.refreshKeys(ctx)
		if key := v.lookupKey(kid); key != nil {
			return key, nil
		}
		return nil, fmt.Errorf("teams: jwt: unknown kid %q", kid)
	}

	token, err := jwt.ParseWithClaims(tokenStr, claims, keyFunc, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return fmt.Errorf("teams: jwt parse: %w", err)
	}
	if token == nil || !token.Valid {
		return fmt.Errorf("teams: invalid jwt")
	}

	iss := strings.TrimSpace(claims.Issuer)
	expectedIss := v.getIssuer()
	if expectedIss != "" && iss != expectedIss {
		return fmt.Errorf("teams: jwt: invalid issuer %q", iss)
	}

	if !audContains(claims.Audience, v.clientID) {
		return fmt.Errorf("teams: jwt: invalid audience")
	}

	ver := strings.TrimSpace(claims.Version)
	switch ver {
	case "1.0":
		if strings.TrimSpace(claims.AppID) != v.clientID {
			return fmt.Errorf("teams: jwt: invalid appid for ver=1.0")
		}
	case "2.0":
		if strings.TrimSpace(claims.AuthorizedParty) != v.clientID && strings.TrimSpace(claims.AppID) != v.clientID {
			return fmt.Errorf("teams: jwt: invalid azp/appid for ver=2.0")
		}
	default:
		// Unknown version; be conservative.
		return fmt.Errorf("teams: jwt: unsupported ver %q", ver)
	}

	if strings.TrimSpace(claims.TenantID) != v.tenantID {
		return fmt.Errorf("teams: jwt: invalid tenant id")
	}

	return nil
}

func audContains(aud jwt.ClaimStrings, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, a := range aud {
		if strings.TrimSpace(a) == want {
			return true
		}
	}
	return false
}

func extractBearerToken(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(parts[0]), "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (v *jwtVerifier) getIssuer() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.issuer
}

func (v *jwtVerifier) ensureOpenID(ctx context.Context) error {
	issuer, jwksURI, ok := v.snapshotOpenID()
	if ok && issuer != "" && jwksURI != "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.openIDURL, nil)
	if err != nil {
		return fmt.Errorf("teams: openid: create request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("teams: openid: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("teams: openid: unexpected HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return fmt.Errorf("teams: openid: read response: %w", err)
	}

	var cfg openIDConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("teams: openid: decode: %w", err)
	}
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.JWKSURI) == "" {
		return fmt.Errorf("teams: openid: missing issuer/jwks_uri")
	}

	v.mu.Lock()
	v.issuer = strings.TrimSpace(cfg.Issuer)
	v.jwksURI = strings.TrimSpace(cfg.JWKSURI)
	v.mu.Unlock()

	return nil
}

func (v *jwtVerifier) snapshotOpenID() (issuer, jwksURI string, ok bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.issuer, v.jwksURI, v.issuer != "" && v.jwksURI != ""
}

func (v *jwtVerifier) ensureKeys(ctx context.Context) error {
	v.mu.RLock()
	hasKeys := len(v.keys) > 0
	fetchedAt := v.jwksFetchedAt
	jwksURI := v.jwksURI
	v.mu.RUnlock()

	if hasKeys && !fetchedAt.IsZero() && v.now().Sub(fetchedAt) < 6*time.Hour {
		return nil
	}
	if strings.TrimSpace(jwksURI) == "" {
		return fmt.Errorf("teams: jwks uri not configured")
	}
	return v.refreshKeys(ctx)
}

func (v *jwtVerifier) refreshKeys(ctx context.Context) error {
	v.mu.RLock()
	jwksURI := v.jwksURI
	v.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return fmt.Errorf("teams: jwks: create request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("teams: jwks: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("teams: jwks: unexpected HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return fmt.Errorf("teams: jwks: read response: %w", err)
	}

	keys, err := parseJWKS(raw)
	if err != nil {
		return err
	}

	v.mu.Lock()
	v.keys = keys
	v.jwksFetchedAt = v.now()
	v.mu.Unlock()
	return nil
}

func (v *jwtVerifier) lookupKey(kid string) *rsa.PublicKey {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.keys[kid]
}

func parseJWKS(raw []byte) (map[string]*rsa.PublicKey, error) {
	var doc jwks
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("teams: jwks: decode: %w", err)
	}

	out := make(map[string]*rsa.PublicKey)
	for _, k := range doc.Keys {
		kid := strings.TrimSpace(k.Kid)
		if kid == "" {
			continue
		}

		pub, err := jwkToRSAPublicKey(k)
		if err != nil {
			continue
		}
		out[kid] = pub
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("teams: jwks: no usable keys")
	}
	return out, nil
}

func jwkToRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	// Prefer x5c chain if present.
	if len(k.X5c) > 0 && strings.TrimSpace(k.X5c[0]) != "" {
		der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k.X5c[0]))
		if err != nil {
			return nil, err
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		if pub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return pub, nil
		}
		return nil, errors.New("non-RSA x5c key")
	}

	// Fallback: n/e.
	nBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(k.N))
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(k.E))
	if err != nil {
		return nil, err
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, errors.New("missing n/e")
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes).Int64()
	if e <= 0 || e > int64(^uint(0)>>1) {
		return nil, errors.New("invalid exponent")
	}

	return &rsa.PublicKey{N: n, E: int(e)}, nil
}
