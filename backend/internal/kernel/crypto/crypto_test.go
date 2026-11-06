package crypto

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("expected Argon2id PHC hash, got %q", h[:12])
	}
	if ok, rehash := VerifyPassword(h, "correct horse battery"); !ok || rehash {
		t.Fatalf("ok=%v rehash=%v", ok, rehash)
	}
	if ok, _ := VerifyPassword(h, "wrong password here"); ok {
		t.Fatal("wrong password accepted")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$AA$AA", "$argon2id$v=19$m=x$AA$AA"} {
		if ok, _ := VerifyPassword(h, "x"); ok {
			t.Fatalf("accepted %q", h)
		}
	}
}

func TestOldParametersRequestRehash(t *testing.T) {
	salt := []byte("somesaltsomesalt")
	k := argon2.IDKey([]byte("old password value"), salt, 1, 8192, 1, 32)
	old := fmt.Sprintf("$argon2id$v=%d$m=8192,t=1,p=1$%s$%s", argon2.Version,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(k))
	ok, rehash := VerifyPassword(old, "old password value")
	if !ok || !rehash {
		t.Fatalf("ok=%v rehash=%v", ok, rehash)
	}
}

func TestTokensAreUniqueAndHashed(t *testing.T) {
	a, ha, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := NewToken()
	if a == b || len(a) < 43 {
		t.Fatalf("weak tokens %q %q", a, b)
	}
	if !bytes.Equal(ha, HashToken(a)) || bytes.Contains(ha, []byte(a)) {
		t.Fatal("hash mismatch")
	}
}

func TestEncryptionBindsToOwner(t *testing.T) {
	k := Keys{Encryption: bytes.Repeat([]byte{1}, 32), HMAC: bytes.Repeat([]byte{2}, 32)}
	c, err := k.Encrypt([]byte("secret"), []byte("user-a"))
	if err != nil {
		t.Fatal(err)
	}
	if p, err := k.Decrypt(c, []byte("user-a")); err != nil || string(p) != "secret" {
		t.Fatalf("decrypt: %v %q", err, p)
	}
	if _, err := k.Decrypt(c, []byte("user-b")); err == nil {
		t.Fatal("ciphertext opened for another owner")
	}
}

func TestCSRFAndPurposeSeparation(t *testing.T) {
	k := Keys{HMAC: bytes.Repeat([]byte{2}, 32)}
	tok := k.CSRFToken("session-1")
	if !k.VerifyCSRF("session-1", tok) || k.VerifyCSRF("session-2", tok) || k.VerifyCSRF("session-1", "") {
		t.Fatal("CSRF check wrong")
	}
	if bytes.Equal(k.HMACValue("a", "x"), k.HMACValue("b", "x")) {
		t.Fatal("purposes not separated")
	}
}
