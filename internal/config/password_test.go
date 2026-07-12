package config

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	h, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if !IsBcryptHash(h) {
		t.Fatal(h)
	}
	if !CheckPassword(h, "secret") {
		t.Fatal("check failed")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("should fail")
	}
	// legacy plaintext
	if !CheckPassword("admin", "admin") {
		t.Fatal("legacy plain")
	}
}

func TestEnsurePasswordHashed(t *testing.T) {
	pw := "admin"
	changed, err := EnsurePasswordHashed(&pw)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !IsBcryptHash(pw) {
		t.Fatal(pw)
	}
	changed, err = EnsurePasswordHashed(&pw)
	if err != nil || changed {
		t.Fatalf("second pass changed=%v err=%v", changed, err)
	}
	if !IsDefaultPassword(pw) {
		t.Fatal("should still match default admin")
	}
}
