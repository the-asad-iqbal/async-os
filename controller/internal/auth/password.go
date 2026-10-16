package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// pbkdf2Iterations follows the OWASP recommendation for PBKDF2-HMAC-SHA256.
// A variable only so tests can run faster.
var pbkdf2Iterations = 600_000

const (
	minPasswordLength = 10
	maxPasswordLength = 256
)

// ErrWeakPassword is returned for passwords that fail the policy.
var ErrWeakPassword = errors.New("password must be 10 to 256 characters and differ from the username")

func checkPasswordPolicy(username, password string) error {
	n := utf8.RuneCountInString(password)
	if n < minPasswordLength || n > maxPasswordLength || strings.EqualFold(password, username) {
		return ErrWeakPassword
	}
	return nil
}

// hashPassword returns "pbkdf2-sha256$iterations$salt$hash".
func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is verified against when a username does not exist, so
// response times do not reveal which usernames are valid.
var dummyHash, _ = hashPassword("asyncos-timing-equalizer")

// SetHashIterationsForTests lowers the PBKDF2 cost so test suites of other
// packages run quickly. Never call it outside tests.
func SetHashIterationsForTests(n int) { pbkdf2Iterations = n }
