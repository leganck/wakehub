package mdns

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const ServiceType = "_wolclient._tcp"

type Peer struct {
	Instance string            `json:"instance"`
	Hostname string            `json:"hostname"`
	Key      string            `json:"key"`
	MAC      string            `json:"mac"`
	IPs      []string          `json:"ips"`
	Port     int               `json:"port"`
	Text     map[string]string `json:"text"`
	SeenAt   int64             `json:"seenAt"`
}

type Browser struct {
	mu    sync.RWMutex
	peers map[string]Peer
}

func NewBrowser() *Browser {
	return &Browser{peers: map[string]Peer{}}
}

func (b *Browser) List() []Peer {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Peer, 0, len(b.peers))
	for _, p := range b.peers {
		out = append(out, p)
	}
	return out
}

func (b *Browser) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	b.scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.scan(ctx)
		}
	}
}

func (b *Browser) scan(ctx context.Context) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return
	}
	entries := make(chan *zeroconf.ServiceEntry, 16)
	scanCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	go func() {
		for e := range entries {
			txt := map[string]string{}
			for _, t := range e.Text {
				if k, v, ok := splitKV(t); ok {
					txt[k] = v
				}
			}
			ips := []string{}
			for _, ip := range e.AddrIPv4 {
				ips = append(ips, ip.String())
			}
			for _, ip := range e.AddrIPv6 {
				ips = append(ips, ip.String())
			}
			p := Peer{
				Instance: e.Instance,
				Hostname: firstNonEmpty(txt["hostname"], e.HostName),
				Key:      txt["key"],
				MAC:      txt["mac"],
				IPs:      ips,
				Port:     e.Port,
				Text:     txt,
				SeenAt:   time.Now().Unix(),
			}
			id := p.Instance
			if p.Key != "" {
				id = p.Key
			}
			b.mu.Lock()
			b.peers[id] = p
			b.mu.Unlock()
		}
	}()
	_ = resolver.Browse(scanCtx, ServiceType, "local.", entries)
	// prune stale (> 2 min)
	cutoff := time.Now().Unix() - 120
	b.mu.Lock()
	for k, p := range b.peers {
		if p.SeenAt < cutoff {
			delete(b.peers, k)
		}
	}
	b.mu.Unlock()
}

func splitKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Publisher announces this client on the LAN.
type Publisher struct {
	server *zeroconf.Server
}

func Publish(instance, key, hostname, mac string, port int) (*Publisher, error) {
	if instance == "" {
		instance = fmt.Sprintf("wol-%s", key)
	}
	if port <= 0 {
		port = 9
	}
	txt := []string{
		"key=" + key,
		"hostname=" + hostname,
		"mac=" + mac,
	}
	s, err := zeroconf.Register(instance, ServiceType, "local.", port, txt, nil)
	if err != nil {
		return nil, err
	}
	return &Publisher{server: s}, nil
}

func (p *Publisher) Shutdown() {
	if p != nil && p.server != nil {
		p.server.Shutdown()
	}
}

// PrimaryMAC picks first non-empty hardware addr from interfaces.
func PrimaryMAC() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) != 6 {
			continue
		}
		return iface.HardwareAddr.String()
	}
	return ""
}
