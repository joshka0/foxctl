package authbroker

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	plain := []byte(`{"access_token":"abc","token_type":"bearer"}`)

	enc, err := Encrypt(key, plain)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if len(enc) == 0 {
		t.Fatalf("Encrypt() returned empty ciphertext")
	}

	dec, err := Decrypt(key, enc)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatalf("decrypt mismatch: got %q want %q", string(dec), string(plain))
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	keyA := bytes.Repeat([]byte{0x01}, 32)
	keyB := bytes.Repeat([]byte{0x02}, 32)

	enc, err := Encrypt(keyA, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if _, err := Decrypt(keyB, enc); err == nil {
		t.Fatalf("Decrypt() expected error for wrong key")
	}
}

func TestDecrypt_TamperedCiphertextFails(t *testing.T) {
	key := bytes.Repeat([]byte{0x03}, 32)

	enc, err := Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	enc[len(enc)-1] ^= 0xFF

	if _, err := Decrypt(key, enc); err == nil {
		t.Fatalf("Decrypt() expected error for tampered ciphertext")
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	key := bytes.Repeat([]byte{0x04}, 32)

	enc, err := Encrypt(key, nil)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	dec, err := Decrypt(key, enc)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if len(dec) != 0 {
		t.Fatalf("expected empty plaintext, got len=%d", len(dec))
	}
}

func TestValidateKey(t *testing.T) {
	if err := ValidateKey(bytes.Repeat([]byte{0x01}, 31)); err == nil {
		t.Fatalf("ValidateKey(short) expected error")
	}
	if err := ValidateKey(bytes.Repeat([]byte{0x01}, 33)); err == nil {
		t.Fatalf("ValidateKey(long) expected error")
	}
	if err := ValidateKey(bytes.Repeat([]byte{0x01}, 32)); err != nil {
		t.Fatalf("ValidateKey(32) unexpected error: %v", err)
	}
}

func TestDecodeKey(t *testing.T) {
	raw := bytes.Repeat([]byte{0xAA}, 32)
	encoded := base64.StdEncoding.EncodeToString(raw)

	got, err := DecodeKey(encoded)
	if err != nil {
		t.Fatalf("DecodeKey() error = %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("DecodeKey() mismatch")
	}

	if _, err := DecodeKey("not-base64!"); err == nil {
		t.Fatalf("DecodeKey(invalid base64) expected error")
	}

	short := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, 16))
	if _, err := DecodeKey(short); err == nil {
		t.Fatalf("DecodeKey(short key) expected error")
	}
}
