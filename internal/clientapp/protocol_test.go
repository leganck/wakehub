package clientapp

import (
	"errors"
	"testing"
)

func TestNormalizeWS(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"127.0.0.1:8080", "ws://127.0.0.1:8080/api/ws/client"},
		{"http://host:9/api/ws/client", "ws://host:9/api/ws/client"},
		{"https://host/path", "wss://host/path"},
		{"ws://h:1/", "ws://h:1/api/ws/client"},
	}
	for _, c := range cases {
		got, err := NormalizeWS(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%q => %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := NormalizeWS("ftp://x"); err == nil {
		t.Fatal("expected scheme error")
	}
}

func TestHandleHelloResponse(t *testing.T) {
	if err := HandleHelloResponse([]byte(`{"type":"hello_ok"}`)); err != nil {
		t.Fatal(err)
	}
	err := HandleHelloResponse([]byte(`{"type":"error","message":"token mismatch"}`))
	if !IsFatal(err) {
		t.Fatalf("want fatal, got %v", err)
	}
	err = HandleHelloResponse([]byte(`{"type":"error","message":"other"}`))
	if IsFatal(err) {
		t.Fatal("non-token error should not be fatal")
	}
	var f *FatalError
	if !errors.As(HandleHelloResponse([]byte(`{"type":"error","message":"invalid hello"}`)), &f) {
		t.Fatal("want fatal for invalid hello")
	}
}

func TestSanitizeInstance(t *testing.T) {
	if SanitizeInstance("My PC") != "My-PC" {
		t.Fatal(SanitizeInstance("My PC"))
	}
	if SanitizeInstance("") != "client" {
		t.Fatal(SanitizeInstance(""))
	}
}
