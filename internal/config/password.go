package config

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = bcrypt.DefaultCost

// IsBcryptHash reports whether s looks like a bcrypt hash.
func IsBcryptHash(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "$2a$") ||
		strings.HasPrefix(s, "$2b$") ||
		strings.HasPrefix(s, "$2y$")
}

// HashPassword returns a bcrypt hash of plain.
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword compares a stored hash (or legacy plaintext) with a candidate.
func CheckPassword(stored, plain string) bool {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return false
	}
	if IsBcryptHash(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)) == nil
	}
	// Legacy plaintext (pre-hash configs).
	return subtleConstantEqual(stored, plain)
}

// EnsurePasswordHashed rewrites plaintext stored passwords to bcrypt.
// Returns true if the value was changed.
func EnsurePasswordHashed(stored *string) (changed bool, err error) {
	if stored == nil {
		return false, nil
	}
	pw := strings.TrimSpace(*stored)
	if pw == "" {
		return false, nil
	}
	if IsBcryptHash(pw) {
		return false, nil
	}
	h, err := HashPassword(pw)
	if err != nil {
		return false, err
	}
	*stored = h
	return true, nil
}

// IsDefaultPassword reports whether stored password verifies as DefaultAuthPassword.
func IsDefaultPassword(stored string) bool {
	return CheckPassword(stored, DefaultAuthPassword)
}

func subtleConstantEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
