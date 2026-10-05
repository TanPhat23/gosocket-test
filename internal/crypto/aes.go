package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

var (
	ErrInvalidKeySize    = errors.New("master key must be exactly 32 bytes for AES-256")
	ErrCiphertextTooShort = errors.New("ciphertext too short: must contain at least nonce and tag")
	ErrDecryptionFailed  = errors.New("failed to decrypt ciphertext: invalid data or authentication failure")
)

// SecretCipher provides authenticated AES-256-GCM encryption and decryption.
type SecretCipher struct {
	aead           cipher.AEAD
	keyFingerprint string
}

// NewSecretCipher constructs a SecretCipher with the given 32-byte master key.
// It computes a 16-character hex fingerprint from the SHA-256 digest of the master key.
func NewSecretCipher(masterKey []byte) (*SecretCipher, error) {
	if len(masterKey) != 32 {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM AEAD: %w", err)
	}

	hash := sha256.Sum256(masterKey)
	fingerprint := hex.EncodeToString(hash[:])[:16]

	return &SecretCipher{
		aead:           gcm,
		keyFingerprint: fingerprint,
	}, nil
}

// KeyFingerprint returns the 16-character hex fingerprint of the encryption key.
func (c *SecretCipher) KeyFingerprint() string {
	return c.keyFingerprint
}

// Encrypt encrypts plaintext using AES-256-GCM with a fresh random 96-bit (12-byte) nonce.
// It returns base64.StdEncoding(nonce || ciphertext || tag).
func (c *SecretCipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize()) // 12 bytes for GCM
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate random nonce: %w", err)
	}

	// aead.Seal appends ciphertext and 16-byte authentication tag to dst (which starts with nonce)
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decodes base64-encoded payload (nonce || ciphertext || tag) and authenticates/decrypts it.
func (c *SecretCipher) Decrypt(encodedCiphertext string) (string, error) {
	if encodedCiphertext == "" {
		return "", ErrCiphertextTooShort
	}

	data, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return "", fmt.Errorf("invalid base64 ciphertext: %w", err)
	}

	nonceSize := c.aead.NonceSize()
	overhead := c.aead.Overhead()
	if len(data) < nonceSize+overhead {
		return "", ErrCiphertextTooShort
	}

	nonce := data[:nonceSize]
	ciphertextWithTag := data[nonceSize:]

	plaintext, err := c.aead.Open(nil, nonce, ciphertextWithTag, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	return string(plaintext), nil
}
