package clientlink

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leganck/wakehub/internal/config"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Hello struct {
	Type     string          `json:"type"`
	Key      string          `json:"key"`
	Token    string          `json:"token"`
	Hostname string          `json:"hostname"`
	NICs     []config.NICInfo `json:"nics"`
}

type ServerMsg struct {
	Type string `json:"type"`
}

type ClientInfo struct {
	Key         string           `json:"key"`
	Hostname    string           `json:"hostname"`
	NICs        []config.NICInfo `json:"nics"`
	Remote      string           `json:"remote"`
	Connected   int64            `json:"connectedAt"`
	LastSeen    int64            `json:"lastSeen"`
	LastEvent   string           `json:"lastEvent,omitempty"`   // e.g. shutdown_ack / shutdown_err
	LastError   string           `json:"lastError,omitempty"`
	LastEventAt int64            `json:"lastEventAt,omitempty"`
}

type connEntry struct {
	info ClientInfo
	conn *websocket.Conn
	mu   sync.Mutex
}

type Hub struct {
	mu          sync.RWMutex
	clients     map[string]*connEntry
	token       string
	onChange    func()
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

func (h *Hub) Shutdown(key string) error {
	h.mu.RLock()
	c, ok := h.clients[key]
	h.mu.RUnlock()
	if !ok {
		return errors.New("client offline or not bound")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	msg, _ := json.Marshal(ServerMsg{Type: "shutdown"})
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, msg)
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
	var hello Hello
	if err := json.Unmarshal(data, &hello); err != nil || hello.Type != "hello" || hello.Key == "" {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"invalid hello"}`))
		return
	}
	h.mu.RLock()
	token := h.token
	h.mu.RUnlock()
	if token != "" && hello.Token != token {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"token mismatch"}`))
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
		},
	}
	h.mu.Lock()
	if old, ok := h.clients[hello.Key]; ok {
		_ = old.conn.Close()
	}
	h.clients[hello.Key] = entry
	h.mu.Unlock()
	h.notify()
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello_ok"}`))

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
		if mt == websocket.TextMessage {
			var m map[string]any
			if json.Unmarshal(msg, &m) != nil {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "ping":
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
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
			case "shutdown_ack", "shutdown_err":
				errMsg, _ := m["error"].(string)
				h.mu.Lock()
				if e, ok := h.clients[hello.Key]; ok {
					e.info.LastEvent = t
					e.info.LastError = errMsg
					e.info.LastEventAt = time.Now().Unix()
					e.info.LastSeen = time.Now().Unix()
				}
				h.mu.Unlock()
				if t == "shutdown_ack" {
					log.Printf("client %s shutdown_ack", hello.Key)
				} else {
					log.Printf("client %s shutdown_err: %s", hello.Key, errMsg)
				}
			default:
				// ignore unknown types
			}
		}
	}

	h.mu.Lock()
	if cur, ok := h.clients[hello.Key]; ok && cur.conn == conn {
		delete(h.clients, hello.Key)
	}
	h.mu.Unlock()
	h.notify()
}
