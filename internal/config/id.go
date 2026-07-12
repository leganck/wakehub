package config

import (
	"crypto/rand"
	"encoding/hex"
)

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte("fallbackid"))[:n*2]
	}
	return hex.EncodeToString(b)[:n]
}

// RandomID exports random id helper for other packages.
func RandomID(n int) string { return randomID(n) }

// RandomToken returns a URL-safe hex token of approx length characters
// (even length preferred; odd lengths are truncated from hex).
func RandomToken(length int) string {
	if length <= 0 {
		length = 32
	}
	// each byte -> 2 hex chars
	n := (length + 1) / 2
	if n < 8 {
		n = 8
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return randomID(16)
	}
	s := hex.EncodeToString(b)
	if len(s) > length {
		return s[:length]
	}
	return s
}
