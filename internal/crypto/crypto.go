// Package crypto mirrors the token/hash conventions of the Immich server:
// bcrypt(10) passwords, opaque 32-byte session/API-key secrets, SHA-256 of
// secrets at rest and SHA-1 asset checksums.
package crypto

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"regexp"

	"golang.org/x/crypto/bcrypt"
)

// SaltRounds matches Immich's bcrypt cost (src/constants.ts SALT_ROUNDS).
const SaltRounds = 10

var nonWord = regexp.MustCompile(`[^\w]`)

// NewUUID returns a random RFC 4122 version 4 UUID string.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hex[v>>4], hex[v&0x0f])
	}
	return string(out)
}

// RandomToken generates an opaque secret the same way the upstream server
// does: 32 random bytes, base64 encoded, with every non-word character
// stripped (crypto.repository randomBytesAsText).
func RandomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return nonWord.ReplaceAllString(base64.StdEncoding.EncodeToString(b), "")
}

// HashToken returns the raw SHA-256 digest stored server-side for a secret.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// HashPassword hashes a secret with bcrypt at the Immich default cost.
func HashPassword(secret string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(secret), SaltRounds)
	return string(b), err
}

// ComparePassword verifies a secret against a bcrypt hash. It returns false
// for empty hashes (users without a password can never log in via password).
func ComparePassword(secret, hash string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret)) == nil
}

// Sha1B64 streams r through SHA-1 and returns the URL-safe base64 digest —
// the format the x-immich-checksum header and bulk upload check use.
func Sha1B64(r io.Reader) (string, []byte, error) {
	h := sha1.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", nil, err
	}
	sum := h.Sum(nil)
	return base64.StdEncoding.EncodeToString(sum), sum, nil
}

// DecodeB64SHA1 parses a client-supplied base64 SHA-1 checksum.
func DecodeB64SHA1(s string) ([]byte, bool) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != sha1.Size {
		return nil, false
	}
	return b, true
}
