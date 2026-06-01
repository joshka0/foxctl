package authbroker

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"testing/quick"
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

func TestEncryptDecryptPropertyRoundTripAndCiphertextShape(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(seed string, plaintext []byte) bool {
		key := authbrokerTestKey(seed)
		ciphertext, err := Encrypt(key, plaintext)
		if err != nil {
			return false
		}
		if len(ciphertext) != 12+len(plaintext)+16 {
			return false
		}

		got, err := Decrypt(key, ciphertext)
		if err != nil {
			return false
		}
		return bytes.Equal(got, plaintext)
	}, cfg)
	if err != nil {
		t.Fatalf("encrypt/decrypt property failed: %v", err)
	}
}

func TestDecryptPropertyRejectsAnyTamperedCiphertextByte(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(seed string, plaintext []byte, indexSeed uint64) bool {
		key := authbrokerTestKey(seed)
		ciphertext, err := Encrypt(key, plaintext)
		if err != nil {
			return false
		}
		if len(ciphertext) == 0 {
			return false
		}

		tampered := append([]byte(nil), ciphertext...)
		idx := int(indexSeed % uint64(len(tampered)))
		tampered[idx] ^= 0x80

		_, err = Decrypt(key, tampered)
		return err != nil
	}, cfg)
	if err != nil {
		t.Fatalf("tampered ciphertext property failed: %v", err)
	}
}

func TestEncryptPropertyUsesFreshNonceForSamePlaintext(t *testing.T) {
	t.Parallel()

	key := authbrokerTestKey("fresh nonce")
	plaintext := []byte("same plaintext")
	seenNonces := map[string]struct{}{}
	seenCiphertexts := map[string]struct{}{}

	for i := 0; i < 32; i++ {
		ciphertext, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		if len(ciphertext) < 12 {
			t.Fatalf("ciphertext too short: len=%d", len(ciphertext))
		}

		nonce := string(ciphertext[:12])
		if _, exists := seenNonces[nonce]; exists {
			t.Fatalf("nonce reused at iteration %d", i)
		}
		seenNonces[nonce] = struct{}{}

		full := string(ciphertext)
		if _, exists := seenCiphertexts[full]; exists {
			t.Fatalf("ciphertext reused at iteration %d", i)
		}
		seenCiphertexts[full] = struct{}{}
	}
}

func TestDecryptRejectsTruncatedCiphertexts(t *testing.T) {
	t.Parallel()

	key := authbrokerTestKey("truncated")
	ciphertext, err := Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	for length := 0; length < len(ciphertext); length++ {
		truncated := append([]byte(nil), ciphertext[:length]...)
		if _, err := Decrypt(key, truncated); err == nil {
			t.Fatalf("Decrypt() accepted truncated ciphertext len=%d original=%d", length, len(ciphertext))
		}
	}
}

func TestValidateKeyPropertyOnlyAcceptsExactly32Bytes(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(raw []byte) bool {
		key := append([]byte(nil), raw...)
		err := ValidateKey(key)
		return (len(key) == 32 && err == nil) || (len(key) != 32 && err != nil)
	}, cfg)
	if err != nil {
		t.Fatalf("validate key property failed: %v", err)
	}
}

func TestDecodeKeyTrimsWhitespaceAndRejectsWrongDecodedLength(t *testing.T) {
	t.Parallel()

	raw := authbrokerTestKey("decode")
	encoded := "\n\t " + base64.StdEncoding.EncodeToString(raw) + " \t\n"
	decoded, err := DecodeKey(encoded)
	if err != nil {
		t.Fatalf("DecodeKey() error = %v", err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatalf("DecodeKey() = %x, want %x", decoded, raw)
	}

	for _, length := range []int{0, 1, 16, 31, 33, 64} {
		encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, length))
		if _, err := DecodeKey(encoded); err == nil {
			t.Fatalf("DecodeKey() accepted decoded key length %d", length)
		}
	}
}

func authbrokerTestKey(seed string) []byte {
	sum := sha256.Sum256([]byte(seed))
	return sum[:]
}
