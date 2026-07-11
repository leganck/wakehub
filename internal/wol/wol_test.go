package wol

import "testing"

func TestMagicPacket(t *testing.T) {
	mp, err := NewMagicPacket("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatal(err)
	}
	b, err := mp.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 102 {
		t.Fatalf("len=%d", len(b))
	}
	for i := 0; i < 6; i++ {
		if b[i] != 0xFF {
			t.Fatalf("header %d", i)
		}
	}
	want := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	for g := 0; g < 16; g++ {
		off := 6 + g*6
		for i := 0; i < 6; i++ {
			if b[off+i] != want[i] {
				t.Fatalf("payload group %d", g)
			}
		}
	}
}
