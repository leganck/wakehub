package notify

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/leganck/wakehub/internal/config"
)

type Event struct {
	Event      string `json:"event"`
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	GroupID    string `json:"groupId,omitempty"`
	GroupName  string `json:"groupName,omitempty"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Time       int64  `json:"time"`
}

type Sender struct {
	store  *config.Store
	client *http.Client
}

func New(store *config.Store) *Sender {
	return &Sender{
		store:  store,
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

func (s *Sender) Send(ev Event) {
	st := s.store.Settings()
	url := strings.TrimSpace(st.NotifyWebhook)
	if url == "" {
		return
	}
	switch ev.Event {
	case "wake":
		if !st.NotifyOnWake {
			return
		}
	case "shutdown":
		if !st.NotifyOnShutdown {
			return
		}
	case "probe":
		if !st.NotifyOnProbeChange {
			return
		}
	case "schedule":
		if !st.NotifyOnSchedule {
			return
		}
	}
	if ev.Time == 0 {
		ev.Time = time.Now().Unix()
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("notify webhook: %v", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("notify webhook status %d", resp.StatusCode)
	}
}
