package audit

import "testing"

func TestRingNewestFirst(t *testing.T) {
	r := New(4)
	r.Add(Entry{Event: "a", At: 1})
	r.Add(Entry{Event: "b", At: 2})
	r.Add(Entry{Event: "c", At: 3})
	list := r.List(10)
	if len(list) != 3 || list[0].Event != "c" || list[2].Event != "a" {
		t.Fatalf("%+v", list)
	}
	r.Add(Entry{Event: "d", At: 4})
	r.Add(Entry{Event: "e", At: 5})
	list = r.List(10)
	if len(list) != 4 || list[0].Event != "e" {
		t.Fatalf("%+v", list)
	}
}
