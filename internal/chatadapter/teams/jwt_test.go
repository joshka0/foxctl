package teams

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTVerifier_Verify_v1Token(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	const (
		clientID = "client-id"
		tenantID = "tenant-id"
		issuer   = "https://issuer.example.test"
		kid      = "kid-1"
	)

	jwksJSON := map[string]any{
		"keys": []any{
			map[string]any{
				"kty": "RSA",
				"use": "sig",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
			},
		},
	}

	var openIDURL string
	var jwksURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openid":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":   issuer,
				"jwks_uri": jwksURL,
			})
		case "/jwks":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jwksJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	openIDURL = srv.URL + "/openid"
	jwksURL = srv.URL + "/jwks"

	claims := botClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  []string{clientID},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
		},
		Version:  "1.0",
		AppID:    clientID,
		TenantID: tenantID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid

	tokenStr, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	v := newJWTVerifier(clientID, tenantID, srv.Client())
	v.openIDURL = openIDURL

	if err := v.Verify(context.Background(), "Bearer "+tokenStr); err != nil {
		t.Fatalf("Verify err: %v", err)
	}
}

func TestJWTVerifier_Verify_InvalidTenant(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	const (
		clientID = "client-id"
		tenantID = "tenant-id"
		issuer   = "https://issuer.example.test"
		kid      = "kid-1"
	)

	jwksJSON := map[string]any{
		"keys": []any{
			map[string]any{
				"kty": "RSA",
				"use": "sig",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
			},
		},
	}

	var openIDURL string
	var jwksURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openid":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":   issuer,
				"jwks_uri": jwksURL,
			})
		case "/jwks":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jwksJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	openIDURL = srv.URL + "/openid"
	jwksURL = srv.URL + "/jwks"

	claims := botClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Audience:  []string{clientID},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
		Version:  "1.0",
		AppID:    clientID,
		TenantID: "wrong-tenant",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid

	tokenStr, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	v := newJWTVerifier(clientID, tenantID, srv.Client())
	v.openIDURL = openIDURL

	if err := v.Verify(context.Background(), "Bearer "+tokenStr); err == nil {
		t.Fatalf("expected Verify error, got nil")
	}
}
