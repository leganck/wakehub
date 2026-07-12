package clientapp

import (
	"testing"
	"time"
)

func TestNextBackoff(t *testing.T) {
	if NextBackoff(3*time.Second, 60*time.Second) != 6*time.Second {
		t.Fatal(NextBackoff(3*time.Second, 60*time.Second))
	}
	if NextBackoff(40*time.Second, 60*time.Second) != 60*time.Second {
		t.Fatal(NextBackoff(40*time.Second, 60*time.Second))
	}
}

func TestWithJitter(t *testing.T) {
	base := 10 * time.Second
	if WithJitter(base, 0, 99) != base {
		t.Fatal("pct 0")
	}
	got := WithJitter(base, 20, 10)
	if got < base || got > base+2*time.Second {
		t.Fatalf("jitter out of range: %v", got)
	}
}
