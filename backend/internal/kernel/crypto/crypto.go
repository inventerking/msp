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

// DefaultKeyID names the field-encryption key when no ID is configured. Data written before key IDs
// were embedded in ciphertexts was written with this key.
const DefaultKeyID = "env-1"

// marker starts a ciphertext that names its key: marker, ID length, ID, nonce, sealed data. Older
// ciphertexts start straight with the nonce; Decrypt still reads them.
const marker = 0xFE

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
	// EncryptionID names Encryption. Empty means DefaultKeyID.
	EncryptionID string
	// Previous are retired encryption keys by ID, kept only to read data not yet re-encrypted.
	Previous map[string][]byte
}

// ID is the name of the key new data is encrypted with.
func (k Keys) ID() string {
	if k.EncryptionID == "" {
		return DefaultKeyID
	}
	return k.EncryptionID
}

func (k Keys) byID(id string) []byte {
	if id == k.ID() {
		return k.Encryption
	}
	return k.Previous[id]
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

// Encrypt seals a secret with AES-256-GCM under the current key, and names the key in the result.
// The additional data binds it to its owner.
func (k Keys) Encrypt(plaintext, additional []byte) ([]byte, error) {
	id := k.ID()
	if len(id) == 0 || len(id) > 32 {
		return nil, errors.New("encryption key ID must be 1 to 32 bytes")
	}
	gcm, err := sealer(k.Encryption)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{marker, byte(len(id))}, id...) //nolint:gosec // Checked above: 1 to 32 bytes.
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plaintext, additional), nil
}

// Decrypt opens a secret sealed by Encrypt with the current or any previous key. Ciphertexts from
// before keys were named are tried against every key.
func (k Keys) Decrypt(ciphertext, additional []byte) ([]byte, error) {
	if id, body, ok := splitNamed(ciphertext); ok {
		if key := k.byID(id); key != nil {
			if plain, err := open(key, body, additional); err == nil {
				return plain, nil
			}
		}
	}
	for _, key := range k.all() {
		if plain, err := open(key, ciphertext, additional); err == nil {
			return plain, nil
		}
	}
	return nil, errors.New("cannot decrypt: no configured key opens this value")
}

// NeedsReencrypt reports whether a ciphertext is not yet under the current key.
func (k Keys) NeedsReencrypt(ciphertext []byte) bool {
	id, _, ok := splitNamed(ciphertext)
	return !ok || id != k.ID()
}

// KeyIDOf names the key a ciphertext says it was sealed with, or DefaultKeyID for older values.
func KeyIDOf(ciphertext []byte) string {
	if id, _, ok := splitNamed(ciphertext); ok {
		return id
	}
	return DefaultKeyID
}

func (k Keys) all() [][]byte {
	out := [][]byte{k.Encryption}
	for _, key := range k.Previous {
		out = append(out, key)
	}
	return out
}

func splitNamed(c []byte) (string, []byte, bool) {
	if len(c) < 2 || c[0] != marker || len(c) < 2+int(c[1]) {
		return "", nil, false
	}
	return string(c[2 : 2+int(c[1])]), c[2+int(c[1]):], true
}

func open(key, ciphertext, additional []byte) ([]byte, error) {
	gcm, err := sealer(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, body := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, additional)
}

func sealer(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
