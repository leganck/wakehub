package probe

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/leganck/wakehub/internal/config"
)

type Result struct {
	DeviceID  string `json:"deviceId"`
	Online    bool   `json:"online"`
	Method    string `json:"method"`
	Target    string `json:"target"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Error     string `json:"error,omitempty"`
	CheckedAt int64  `json:"checkedAt"`
}

type StatusCache struct {
	mu   sync.RWMutex
	byID map[string]Result
}

func NewStatusCache() *StatusCache {
	return &StatusCache{byID: map[string]Result{}}
}

func (c *StatusCache) Get(id string) (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.byID[id]
	return r, ok
}

func (c *StatusCache) Set(r Result) (prev Result, had bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, had = c.byID[r.DeviceID]
	c.byID[r.DeviceID] = r
	return prev, had
}

func (c *StatusCache) Snapshot() []Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Result, 0, len(c.byID))
	for _, r := range c.byID {
		out = append(out, r)
	}
	return out
}

// ResolveTarget picks host for probing.
func ResolveTarget(d config.Device, nics []config.NICInfo) (host string, port int, method string) {
	method = strings.ToLower(strings.TrimSpace(d.ProbeMethod))
	if method == "" || method == "off" || method == "none" {
		return "", 0, "off"
	}
	host = strings.TrimSpace(d.ProbeHost)
	if host == "" {
		// preferred NIC IPv4
		if d.PreferredNIC != "" {
			for _, n := range nics {
				if n.Name == d.PreferredNIC && len(n.IPv4) > 0 {
					host = n.IPv4[0]
					break
				}
			}
		}
		if host == "" {
			for _, n := range nics {
				if len(n.IPv4) > 0 {
					host = n.IPv4[0]
					break
				}
			}
		}
	}
	port = d.ProbePort
	if method == "tcp" && port <= 0 {
		port = 3389
	}
	return host, port, method
}

func ProbeOnce(d config.Device, nics []config.NICInfo) Result {
	host, port, method := ResolveTarget(d, nics)
	res := Result{
		DeviceID:  d.ID,
		Method:    method,
		CheckedAt: time.Now().Unix(),
	}
	if method == "off" {
		res.Error = "probe disabled"
		return res
	}
	if host == "" {
		res.Error = "no probe host"
		return res
	}
	start := time.Now()
	var err error
	switch method {
	case "tcp":
		res.Target = fmt.Sprintf("%s:%d", host, port)
		err = probeTCP(host, port, 2*time.Second)
	case "icmp", "ping":
		res.Target = host
		res.Method = "icmp"
		err = probeICMP(host, 3*time.Second)
	default:
		res.Error = "unknown probe method"
		return res
	}
	res.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		res.Online = false
		res.Error = err.Error()
		return res
	}
	res.Online = true
	return res
}

func probeTCP(host string, port int, timeout time.Duration) error {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func probeICMP(host string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", "2000", host)
	} else {
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", "2", host)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%v: %s", err, msg)
	}
	return nil
}

// Runner periodically probes enabled devices.
type Runner struct {
	store  *config.Store
	cache  *StatusCache
	nicsFn func(deviceID string) []config.NICInfo
	onChange func(prev, cur Result)
}

func NewRunner(store *config.Store, cache *StatusCache, nicsFn func(string) []config.NICInfo, onChange func(prev, cur Result)) *Runner {
	return &Runner{store: store, cache: cache, nicsFn: nicsFn, onChange: onChange}
}

func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	r.tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick()
		}
	}
}

func (r *Runner) tick() {
	now := time.Now().Unix()
	for _, d := range r.store.ListDevices() {
		method := strings.ToLower(strings.TrimSpace(d.ProbeMethod))
		if method == "" || method == "off" || method == "none" {
			continue
		}
		interval := d.ProbeInterval
		if interval < 15 {
			interval = 60
		}
		if prev, ok := r.cache.Get(d.ID); ok && now-prev.CheckedAt < int64(interval) {
			continue
		}
		var nics []config.NICInfo
		if r.nicsFn != nil {
			nics = r.nicsFn(d.ID)
		}
		cur := ProbeOnce(d, nics)
		prev, had := r.cache.Set(cur)
		if r.onChange != nil && (!had || prev.Online != cur.Online) {
			r.onChange(prev, cur)
		}
	}
}

// ProbeDevice forces an immediate probe.
func (r *Runner) ProbeDevice(id string) (Result, error) {
	d, ok := r.store.GetDevice(id)
	if !ok {
		return Result{}, fmt.Errorf("device not found")
	}
	var nics []config.NICInfo
	if r.nicsFn != nil {
		nics = r.nicsFn(id)
	}
	cur := ProbeOnce(d, nics)
	prev, had := r.cache.Set(cur)
	if r.onChange != nil && (!had || prev.Online != cur.Online) {
		r.onChange(prev, cur)
	}
	return cur, nil
}
