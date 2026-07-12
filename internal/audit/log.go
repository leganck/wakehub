// Package audit keeps a small in-memory ring of operational events.
package audit

import (
	"sync"
	"time"
)

// Entry is one audit record.
type Entry struct {
	At         int64  `json:"at"`
	Event      string `json:"event"`
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Ring is a fixed-size concurrent log.
type Ring struct {
	mu   sync.RWMutex
	buf  []Entry
	next int
	size int
	cap  int
}

// New creates a ring with capacity n (min 4, max 2000).
func New(n int) *Ring {
	if n < 4 {
		n = 4
	}
	if n > 2000 {
		n = 2000
	}
	return &Ring{buf: make([]Entry, n), cap: n}
}

// Add appends an entry (fills At if zero).
func (r *Ring) Add(e Entry) {
	if r == nil {
		return
	}
	if e.At == 0 {
		e.At = time.Now().Unix()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = e
	r.next = (r.next + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

// List returns up to limit newest entries (newest first).
func (r *Ring) List(limit int) []Entry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > r.size {
		limit = r.size
	}
	out := make([]Entry, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (r.next - 1 - i + r.cap*2) % r.cap
		out = append(out, r.buf[idx])
	}
	return out
}
