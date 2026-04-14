package authbroker

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// Encrypt encrypts plaintext using AES-256-GCM and returns:
// nonce (12 bytes) || ciphertext || tag.
func Encrypt(key []byte, plaintext []byte) ([]byte, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("authbroker: encrypt: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("authbroker: encrypt: new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("authbroker: encrypt: nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// Decrypt decrypts ciphertext produced by Encrypt (nonce || ciphertext || tag).
func Decrypt(key []byte, ciphertext []byte) ([]byte, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("authbroker: decrypt: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("authbroker: decrypt: new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("authbroker: decrypt: ciphertext too short")
	}

	nonce := ciphertext[:nonceSize]
	body := ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("authbroker: decrypt: open: %w", err)
	}
	return plaintext, nil
}

// ValidateKey validates an AES-256 key length.
func ValidateKey(key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("authbroker: invalid key length: got %d, want 32", len(key))
	}
	return nil
}

// DecodeKey decodes a base64 key and validates it is exactly 32 bytes.
func DecodeKey(base64Key string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64Key))
	if err != nil {
		return nil, fmt.Errorf("authbroker: decode key: %w", err)
	}
	if err := ValidateKey(decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}
