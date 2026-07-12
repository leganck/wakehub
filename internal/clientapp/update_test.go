package clientapp

import "testing"

func TestIsNewerVersion(t *testing.T) {
	if !isNewerVersion("0.2.0", "0.1.6") {
		t.Fatal("0.2 > 0.1.6")
	}
	if isNewerVersion("0.1.6", "0.1.6") {
		t.Fatal("equal")
	}
	if isNewerVersion("0.1.5", "0.1.6") {
		t.Fatal("older")
	}
	if !isNewerVersion("1.0.0", "dev") {
		t.Fatal("dev should update")
	}
}

func TestClientAssetNameShape(t *testing.T) {
	n := clientAssetName()
	if n == "" || !containsAll(n, "wakehub-client_") {
		t.Fatal(n)
	}
}

func containsAll(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && (func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})()))
}
