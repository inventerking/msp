// Package crypto holds password hashing, token generation and field encryption.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. They are stored in each hash, so they can be raised later.
const (
	argonMemoryKiB = 64 * 1024
	argonTime      = 3
	argonThreads   = 2
	argonKeyLen    = 32
	saltLen        = 16
)

// KeyID names the active field-encryption key. Rotation adds a new ID.
const KeyID = "env-1"

// HashPassword returns a PHC-format Argon2id hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	k := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(k)), nil
}

// VerifyPassword checks a password against a PHC-format hash in constant time.
// needsRehash reports that the hash uses older parameters.
func VerifyPassword(encoded, password string) (ok, needsRehash bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false
	}
	var version int
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 128 {
		return false, false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want))) //nolint:gosec // Bounded above.
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	return true, memory != argonMemoryKiB || iterations != argonTime || threads != argonThreads
}

// NewToken returns a 256-bit URL-safe random token and the SHA-256 hash to store.
func NewToken() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken hashes a high-entropy token for storage and lookup.
func HashToken(token string) []byte {
	s := sha256.Sum256([]byte(token))
	return s[:]
}

// Keys holds the keys used for keyed hashes and field encryption.
type Keys struct {
	HMAC       []byte
	Encryption []byte
}

// HMAC returns a keyed hash with a purpose label, so one value cannot be correlated across uses.
func (k Keys) HMACValue(purpose, value string) []byte {
	m := hmac.New(sha256.New, k.HMAC)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write([]byte(value))
	return m.Sum(nil)
}

// CSRFToken derives the CSRF token bound to a session.
func (k Keys) CSRFToken(sessionID string) string {
	return base64.RawURLEncoding.EncodeToString(k.HMACValue("csrf", sessionID))
}

// VerifyCSRF checks a CSRF token in constant time.
func (k Keys) VerifyCSRF(sessionID, token string) bool {
	return subtle.ConstantTimeCompare([]byte(k.CSRFToken(sessionID)), []byte(token)) == 1
}

// Encrypt seals a secret with AES-256-GCM. The additional data binds it to its owner.
func (k Keys) Encrypt(plaintext, additional []byte) ([]byte, error) {
	gcm, err := k.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, additional), nil
}

// Decrypt opens a secret sealed by Encrypt.
func (k Keys) Decrypt(ciphertext, additional []byte) ([]byte, error) {
	gcm, err := k.gcm()
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, body := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, additional)
}

func (k Keys) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(k.Encryption)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
