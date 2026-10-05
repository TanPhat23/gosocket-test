package crypto_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"agent-task-manager/internal/crypto"
)

func TestAES256GCMEncryptDecrypt(t *testing.T) {
	masterKey := []byte("01234567890123456789012345678901") // 32 bytes
	cipher, err := crypto.NewSecretCipher(masterKey)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	secret := "ghp_super_secret_github_personal_access_token_123"
	encrypted, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatalf("unexpected error encrypting: %v", err)
	}

	if encrypted == secret {
		t.Errorf("ciphertext must not equal plaintext")
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("unexpected error decrypting: %v", err)
	}

	if decrypted != secret {
		t.Errorf("expected decrypted %s, got %s", secret, decrypted)
	}
}

func TestSecretCipherKeyValidationAndFingerprint(t *testing.T) {
	// Invalid key lengths
	invalidKeys := [][]byte{
		[]byte(""),
		[]byte("short-key"),
		[]byte("1234567890123456"),                 // 16 bytes
		[]byte("123456789012345678901234567890123"), // 33 bytes
	}

	for _, k := range invalidKeys {
		_, err := crypto.NewSecretCipher(k)
		if err == nil {
			t.Errorf("expected error for key length %d, got nil", len(k))
		}
	}

	// Valid 32-byte key
	masterKey := []byte("01234567890123456789012345678901")
	cipher, err := crypto.NewSecretCipher(masterKey)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	expectedHash := sha256.Sum256(masterKey)
	expectedFingerprint := hex.EncodeToString(expectedHash[:])[:16]
	if cipher.KeyFingerprint() != expectedFingerprint {
		t.Errorf("expected fingerprint %s, got %s", expectedFingerprint, cipher.KeyFingerprint())
	}
}

func TestAES256GCMTamperResistance(t *testing.T) {
	masterKey := []byte("01234567890123456789012345678901")
	cipher, err := crypto.NewSecretCipher(masterKey)
	if err != nil {
		t.Fatalf("unexpected error creating cipher: %v", err)
	}

	secret := "my-api-token"
	encrypted, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatalf("unexpected error encrypting: %v", err)
	}

	// Tampered ciphertext
	tamperedBytes := []byte(encrypted)
	if tamperedBytes[len(tamperedBytes)-1] == 'A' {
		tamperedBytes[len(tamperedBytes)-1] = 'B'
	} else {
		tamperedBytes[len(tamperedBytes)-1] = 'A'
	}

	_, err = cipher.Decrypt(string(tamperedBytes))
	if err == nil {
		t.Errorf("expected decryption failure on tampered data, got nil")
	}

	// Empty string or malformed base64
	_, err = cipher.Decrypt("")
	if err == nil {
		t.Errorf("expected error for empty ciphertext")
	}
	_, err = cipher.Decrypt("not-valid-base64!@#$")
	if err == nil {
		t.Errorf("expected error for invalid base64")
	}
	_, err = cipher.Decrypt("AQID") // too short for nonce (4 bytes decoded < 12 bytes nonce)
	if err == nil {
		t.Errorf("expected error for too short ciphertext")
	}
}
