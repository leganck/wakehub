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
