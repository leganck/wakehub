package clientapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/protocol"
)

// FatalError stops the reconnect loop (auth / permanent config problems).
type FatalError struct {
	Msg  string
	Code string
}

func (e *FatalError) Error() string { return e.Msg }

// IsFatal reports whether err is a FatalError.
func IsFatal(err error) bool {
	var f *FatalError
	return errors.As(err, &f)
}

func asFatal(err error) *FatalError {
	var f *FatalError
	if errors.As(err, &f) {
		return f
	}
	return nil
}

// NormalizeWS expands host:port / http(s) into a websocket client URL.
func NormalizeWS(s string) (string, error) {
	if !strings.Contains(s, "://") {
		s = "ws://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = config.DefaultWSPath
	}
	return u.String(), nil
}

// HandleHelloResponse validates the first server message after hello.
func HandleHelloResponse(msg []byte) error {
	var m map[string]any
	if err := json.Unmarshal(msg, &m); err != nil {
		return fmt.Errorf("invalid hello response: %w", err)
	}
	t, _ := m["type"].(string)
	switch t {
	case protocol.TypeHelloOK:
		return nil
	case protocol.TypeError:
		message, _ := m["message"].(string)
		code, _ := m["code"].(string)
		if message == "" {
			message = string(msg)
		}
		if protocol.IsAuthFailure(code) {
			return &FatalError{Msg: message, Code: code}
		}
		// Legacy servers without code: match message text.
		low := strings.ToLower(message)
		if strings.Contains(low, "token") || strings.Contains(low, "invalid hello") {
			if code == "" {
				if strings.Contains(low, "token") {
					code = protocol.CodeTokenMismatch
				} else {
					code = protocol.CodeInvalidHello
				}
			}
			return &FatalError{Msg: message, Code: code}
		}
		return fmt.Errorf("server error: %s", message)
	default:
		if strings.Contains(string(msg), protocol.TypeHelloOK) {
			return nil
		}
		return fmt.Errorf("unexpected hello response: %s", string(msg))
	}
}
