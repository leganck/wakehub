package clientlink

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/protocol"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// ClientInfo is the public view of a connected client.
type ClientInfo struct {
	Key         string           `json:"key"`
	Hostname    string           `json:"hostname"`
	NICs        []config.NICInfo `json:"nics"`
	Remote      string           `json:"remote"`
	Connected   int64            `json:"connectedAt"`
	LastSeen    int64            `json:"lastSeen"`
	Version     string           `json:"version,omitempty"`
	OS          string           `json:"os,omitempty"`
	Arch        string           `json:"arch,omitempty"`
	LastEvent   string           `json:"lastEvent,omitempty"`
	LastError   string           `json:"lastError,omitempty"`
	LastEventAt int64            `json:"lastEventAt,omitempty"`
}

type connEntry struct {
	info ClientInfo
	conn *websocket.Conn
	mu   sync.Mutex
}

type Hub struct {
	mu       sync.RWMutex
	clients  map[string]*connEntry
	token    string
	onChange func()
}

func NewHub(token string) *Hub {
	return &Hub{clients: map[string]*connEntry{}, token: token}
}

func (h *Hub) SetToken(token string) {
	h.mu.Lock()
	h.token = token
	h.mu.Unlock()
}

func (h *Hub) SetOnChange(fn func()) { h.onChange = fn }

func (h *Hub) notify() {
	if h.onChange != nil {
		h.onChange()
	}
}

func (h *Hub) List() []ClientInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ClientInfo, 0, len(h.clients))
	for _, c := range h.clients {
		out = append(out, c.info)
	}
	return out
}

func (h *Hub) Get(key string) (ClientInfo, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.clients[key]
	if !ok {
		return ClientInfo{}, false
	}
	return c.info, true
}

func (h *Hub) Online(key string) bool {
	if key == "" {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[key]
	return ok
}

// Shutdown sends a power-off command and returns the requestId.
func (h *Hub) Shutdown(key string) (requestID string, err error) {
	return h.SendControl(key, protocol.TypeShutdown)
}

// Restart sends a reboot command and returns the requestId.
func (h *Hub) Restart(key string) (requestID string, err error) {
	return h.SendControl(key, protocol.TypeRestart)
}

// SendControl pushes a typed control message to a connected client.
func (h *Hub) SendControl(key, typ string) (requestID string, err error) {
	h.mu.RLock()
	c, ok := h.clients[key]
	h.mu.RUnlock()
	if !ok {
		return "", errors.New("client offline or not bound")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	reqID := fmt.Sprintf("%s-%d", key, time.Now().UnixNano())
	msg, _ := json.Marshal(protocol.NewControl(typ, reqID))
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		return "", err
	}
	return reqID, nil
}

func writeWSJSON(conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, b)
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return
	}
	var hello protocol.Hello
	if err := json.Unmarshal(data, &hello); err != nil || hello.Type != protocol.TypeHello || hello.Key == "" {
		_ = writeWSJSON(conn, protocol.NewError(protocol.CodeInvalidHello, "invalid hello"))
		return
	}
	h.mu.RLock()
	token := h.token
	h.mu.RUnlock()
	if token != "" && hello.Token != token {
		_ = writeWSJSON(conn, protocol.NewError(protocol.CodeTokenMismatch, "token mismatch"))
		return
	}
	now := time.Now().Unix()
	entry := &connEntry{
		conn: conn,
		info: ClientInfo{
			Key:       hello.Key,
			Hostname:  hello.Hostname,
			NICs:      hello.NICs,
			Remote:    r.RemoteAddr,
			Connected: now,
			LastSeen:  now,
			Version:   hello.Version,
			OS:        hello.OS,
			Arch:      hello.Arch,
		},
	}
	h.mu.Lock()
	if old, ok := h.clients[hello.Key]; ok {
		_ = old.conn.Close()
	}
	h.clients[hello.Key] = entry
	h.mu.Unlock()
	h.notify()
	_ = writeWSJSON(conn, protocol.NewHelloOK())

	conn.SetPongHandler(func(string) error {
		h.mu.Lock()
		if e, ok := h.clients[hello.Key]; ok {
			e.info.LastSeen = time.Now().Unix()
		}
		h.mu.Unlock()
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if mt != websocket.TextMessage {
			continue
		}
		var m map[string]any
		if json.Unmarshal(msg, &m) != nil {
			continue
		}
		t, _ := m["type"].(string)
		switch t {
		case protocol.TypePing:
			_ = writeWSJSON(conn, protocol.NewPong())
			if nics, ok := m["nics"]; ok {
				b, _ := json.Marshal(nics)
				var list []config.NICInfo
				if json.Unmarshal(b, &list) == nil {
					h.mu.Lock()
					if e, ok := h.clients[hello.Key]; ok {
						e.info.NICs = list
						e.info.LastSeen = time.Now().Unix()
						if hn, ok := m["hostname"].(string); ok && hn != "" {
							e.info.Hostname = hn
						}
					}
					h.mu.Unlock()
				}
			} else {
				h.mu.Lock()
				if e, ok := h.clients[hello.Key]; ok {
					e.info.LastSeen = time.Now().Unix()
				}
				h.mu.Unlock()
			}
		case protocol.TypeShutdownAck, protocol.TypeShutdownErr:
			errMsg, _ := m["error"].(string)
			status, _ := m["status"].(string)
			reqID, _ := m["requestId"].(string)
			h.mu.Lock()
			if e, ok := h.clients[hello.Key]; ok {
				e.info.LastEvent = t
				if status != "" {
					e.info.LastEvent = t + ":" + status
				}
				e.info.LastError = errMsg
				e.info.LastEventAt = time.Now().Unix()
				e.info.LastSeen = time.Now().Unix()
			}
			h.mu.Unlock()
			if t == protocol.TypeShutdownAck {
				log.Printf("client %s shutdown_ack status=%s requestId=%s", hello.Key, status, reqID)
			} else {
				log.Printf("client %s shutdown_err requestId=%s: %s", hello.Key, reqID, errMsg)
			}
		default:
			// ignore unknown types
		}
	}

	h.mu.Lock()
	if cur, ok := h.clients[hello.Key]; ok && cur.conn == conn {
		delete(h.clients, hello.Key)
	}
	h.mu.Unlock()
	h.notify()
}
