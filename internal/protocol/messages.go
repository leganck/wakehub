// Package protocol defines the WakeHub client↔server WebSocket message schema.
package protocol

import "github.com/leganck/wakehub/internal/config"

// Message type names (JSON "type" field).
const (
	TypeHello        = "hello"
	TypeHelloOK      = "hello_ok"
	TypeError        = "error"
	TypePing         = "ping"
	TypePong         = "pong"
	TypeShutdown     = "shutdown"
	TypeRestart      = "restart"
	TypeShutdownAck  = "shutdown_ack"
	TypeShutdownErr  = "shutdown_err"
)

// Stable error codes for TypeError (prefer these over parsing message text).
const (
	CodeInvalidHello  = "invalid_hello"
	CodeTokenMismatch = "token_mismatch"
	CodeAuthFailed    = "auth_failed"
)

// Shutdown status values on TypeShutdownAck.
const (
	StatusAccepted  = "accepted"
	StatusExecuted  = "executed"
	StatusDuplicate = "duplicate"
)

// Hello is the first client → server message.
type Hello struct {
	Type     string           `json:"type"`
	Key      string           `json:"key"`
	Token    string           `json:"token,omitempty"`
	Hostname string           `json:"hostname,omitempty"`
	NICs     []config.NICInfo `json:"nics,omitempty"`
	Version  string           `json:"version,omitempty"`
	OS       string           `json:"os,omitempty"`
	Arch     string           `json:"arch,omitempty"`
}

// HelloOK is server → client after successful auth.
type HelloOK struct {
	Type string `json:"type"`
}

// ErrorMsg is server → client for permanent/transient failures.
type ErrorMsg struct {
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// Ping is client → server keepalive + optional NIC refresh.
type Ping struct {
	Type     string           `json:"type"`
	Hostname string           `json:"hostname,omitempty"`
	NICs     []config.NICInfo `json:"nics,omitempty"`
}

// Pong is server → client.
type Pong struct {
	Type string `json:"type"`
}

// Shutdown is server → client power-off request.
type Shutdown struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId,omitempty"`
}

// Control is a generic server → client host action (shutdown/restart).
type Control struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId,omitempty"`
}

// ShutdownAck is client → server acceptance / completion.
type ShutdownAck struct {
	Type      string `json:"type"`
	OK        bool   `json:"ok"`
	Status    string `json:"status,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// ShutdownErr is client → server when the local power-off command failed.
type ShutdownErr struct {
	Type      string `json:"type"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

// NewHelloOK returns a hello_ok payload.
func NewHelloOK() HelloOK { return HelloOK{Type: TypeHelloOK} }

// NewError returns an error payload with code + message.
func NewError(code, message string) ErrorMsg {
	return ErrorMsg{Type: TypeError, Code: code, Message: message}
}

// NewShutdown returns a shutdown control message.
func NewShutdown(requestID string) Shutdown {
	return Shutdown{Type: TypeShutdown, RequestID: requestID}
}

// NewControl returns a control message (TypeShutdown or TypeRestart).
func NewControl(typ, requestID string) Control {
	return Control{Type: typ, RequestID: requestID}
}

// NewPong returns a pong payload.
func NewPong() Pong { return Pong{Type: TypePong} }

// IsAuthFailure reports whether a code is permanent auth/config failure.
func IsAuthFailure(code string) bool {
	switch code {
	case CodeTokenMismatch, CodeInvalidHello, CodeAuthFailed:
		return true
	default:
		return false
	}
}
