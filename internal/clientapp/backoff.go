package clientapp

import "time"

const (
	InitialBackoff = 3 * time.Second
	MaxBackoff     = 60 * time.Second
)

// NextBackoff doubles delay up to max.
func NextBackoff(delay, max time.Duration) time.Duration {
	if delay <= 0 {
		delay = InitialBackoff
	}
	if max <= 0 {
		max = MaxBackoff
	}
	n := delay * 2
	if n > max {
		return max
	}
	return n
}

// WithJitter adds 0..pct percent of delay. pct should be in 0..100.
// seed is any non-negative int64 (e.g. UnixNano).
func WithJitter(delay time.Duration, pct int64, seed int64) time.Duration {
	if pct <= 0 || delay <= 0 {
		return delay
	}
	if pct > 100 {
		pct = 100
	}
	if seed < 0 {
		seed = -seed
	}
	j := seed % (pct + 1)
	return delay + time.Duration(int64(delay)*j/100)
}
