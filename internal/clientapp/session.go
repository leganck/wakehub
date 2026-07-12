package clientapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leganck/wakehub/internal/protocol"
)

// Session dials the server, runs hello + read loop until disconnect or ctx cancel.
// authed is true after a successful hello_ok.
func Session(ctx context.Context, server, token, key, hostname, shutdownCmd, version string) (authed bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	u, err := NormalizeWS(server)
	if err != nil {
		return false, &FatalError{Msg: "invalid server url: " + err.Error()}
	}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	conn, _, err := dialer.DialContext(ctx, u, nil)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	hello := protocol.Hello{
		Type:     protocol.TypeHello,
		Key:      key,
		Token:    token,
		Hostname: hostname,
		NICs:     CollectNICs(),
		Version:  version,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}
	if err := conn.WriteJSON(hello); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, err
	}

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, err
	}
	if err := HandleHelloResponse(msg); err != nil {
		return false, err
	}
	log.Printf("connected as key=%s version=%s", key, version)

	writeMu := make(chan struct{}, 1)
	writeMu <- struct{}{}
	writeJSON := func(v any) error {
		<-writeMu
		defer func() { writeMu <- struct{}{} }()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			switch m["type"] {
			case protocol.TypeShutdown:
				HandleShutdownMsg(m, shutdownCmd, writeJSON)
			case protocol.TypePong:
			case protocol.TypeError:
				log.Printf("server error message: %s", string(data))
			default:
				log.Printf("msg: %s", string(data))
			}
		}
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-done:
			return true, fmt.Errorf("connection closed")
		case <-ticker.C:
			_ = writeJSON(protocol.Ping{
				Type:     protocol.TypePing,
				Hostname: hostname,
				NICs:     CollectNICs(),
			})
		}
	}
}
