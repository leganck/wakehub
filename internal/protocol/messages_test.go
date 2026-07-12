package protocol

import (
	"encoding/json"
	"testing"
)

func TestErrorJSON(t *testing.T) {
	b, err := json.Marshal(NewError(CodeTokenMismatch, "token mismatch"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		t.Fatal("unmarshal")
	}
	if m["type"] != TypeError || m["code"] != CodeTokenMismatch {
		t.Fatalf("%s", b)
	}
}

func TestIsAuthFailure(t *testing.T) {
	if !IsAuthFailure(CodeTokenMismatch) || IsAuthFailure("") {
		t.Fatal("auth codes")
	}
}
