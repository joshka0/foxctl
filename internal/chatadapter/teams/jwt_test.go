package teams

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
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

	openIDURL = "https://openid.invalid/openid"
	jwksURL = "https://openid.invalid/jwks"
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			var payload any
			switch r.URL.String() {
			case openIDURL:
				payload = map[string]any{
					"issuer":   issuer,
					"jwks_uri": jwksURL,
				}
			case jwksURL:
				payload = jwksJSON
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Status:     "404 Not Found",
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader([]byte("not found"))),
					Request:    r,
				}, nil
			}

			buf := bytes.NewBuffer(nil)
			_ = json.NewEncoder(buf).Encode(payload)
			h := make(http.Header)
			h.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     h,
				Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
				Request:    r,
			}, nil
		}),
	}

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

	v := newJWTVerifier(clientID, tenantID, client)
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

	openIDURL = "https://openid.invalid/openid"
	jwksURL = "https://openid.invalid/jwks"
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			var payload any
			switch r.URL.String() {
			case openIDURL:
				payload = map[string]any{
					"issuer":   issuer,
					"jwks_uri": jwksURL,
				}
			case jwksURL:
				payload = jwksJSON
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Status:     "404 Not Found",
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader([]byte("not found"))),
					Request:    r,
				}, nil
			}

			buf := bytes.NewBuffer(nil)
			_ = json.NewEncoder(buf).Encode(payload)
			h := make(http.Header)
			h.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     h,
				Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
				Request:    r,
			}, nil
		}),
	}

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

	v := newJWTVerifier(clientID, tenantID, client)
	v.openIDURL = openIDURL

	if err := v.Verify(context.Background(), "Bearer "+tokenStr); err == nil {
		t.Fatalf("expected Verify error, got nil")
	}
}
