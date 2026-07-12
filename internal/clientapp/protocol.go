package clientapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/leganck/wakehub/internal/config"
)

// FatalError stops the reconnect loop (auth / permanent config problems).
type FatalError struct {
	Msg string
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
	case "hello_ok":
		return nil
	case "error":
		message, _ := m["message"].(string)
		if message == "" {
			message = string(msg)
		}
		low := strings.ToLower(message)
		if strings.Contains(low, "token") || strings.Contains(low, "invalid hello") {
			return &FatalError{Msg: message}
		}
		return fmt.Errorf("server error: %s", message)
	default:
		if strings.Contains(string(msg), "hello_ok") {
			return nil
		}
		return fmt.Errorf("unexpected hello response: %s", string(msg))
	}
}
