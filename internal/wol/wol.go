package wol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

var reMAC = regexp.MustCompile(`(?i)^([0-9a-f]{2}[:-]){5}([0-9a-f]{2})$`)

type MACAddress [6]byte

type MagicPacket struct {
	header  [6]byte
	payload [16]MACAddress
}

func NewMagicPacket(mac string) (*MagicPacket, error) {
	mac = strings.TrimSpace(mac)
	if !reMAC.MatchString(mac) {
		return nil, fmt.Errorf("invalid MAC: %s", mac)
	}
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return nil, err
	}
	if len(hw) != 6 {
		return nil, fmt.Errorf("only MAC-48 supported: %s", mac)
	}
	var mp MagicPacket
	var addr MACAddress
	copy(addr[:], hw)
	for i := range mp.header {
		mp.header[i] = 0xFF
	}
	for i := range mp.payload {
		mp.payload[i] = addr
	}
	return &mp, nil
}

func (mp *MagicPacket) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.BigEndian, mp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Wake sends magic packets. If broadcast is empty, all local IPv4 broadcast addresses are used.
func Wake(mac, broadcast string, port, repeat int) error {
	if port <= 0 || port > 65535 {
		port = 9
	}
	if repeat <= 0 {
		repeat = 3
	}
	mp, err := NewMagicPacket(mac)
	if err != nil {
		return err
	}
	payload, err := mp.Bytes()
	if err != nil {
		return err
	}
	targets := []string{}
	if strings.TrimSpace(broadcast) != "" {
		targets = append(targets, strings.TrimSpace(broadcast))
	} else {
		targets, err = LocalIPv4Broadcasts()
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return fmt.Errorf("no local IPv4 broadcast address found")
		}
	}
	var lastErr error
	sent := 0
	for _, bcast := range targets {
		addr := fmt.Sprintf("%s:%d", bcast, port)
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			lastErr = err
			continue
		}
		for i := 0; i < repeat; i++ {
			conn, err := net.DialUDP("udp", nil, udpAddr)
			if err != nil {
				lastErr = err
				continue
			}
			n, err := conn.Write(payload)
			conn.Close()
			if err != nil {
				lastErr = err
				continue
			}
			if n != 102 {
				lastErr = fmt.Errorf("wrote %d bytes, want 102", n)
				continue
			}
			sent++
			time.Sleep(20 * time.Millisecond)
		}
	}
	if sent == 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("failed to send magic packet")
	}
	return nil
}

func LocalIPv4Broadcasts() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			ip4 := ipn.IP.To4()
			mask := ipn.Mask
			if len(mask) != 4 {
				mask = ip4.DefaultMask()
			}
			if mask == nil {
				continue
			}
			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip4[i] | ^mask[i]
			}
			s := bcast.String()
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	// also try limited broadcast
	if _, ok := seen["255.255.255.255"]; !ok {
		out = append(out, "255.255.255.255")
	}
	return out, nil
}
