package bemfa

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	bemfaapi "github.com/leganck/bemfa-go/api"
	deviceapi "github.com/leganck/bemfa-go/api/device"
	bemfadevice "github.com/leganck/bemfa-go/device"
	"github.com/leganck/bemfa-go/voice"
	"github.com/leganck/wakehub/internal/config"
)

const maxLogEntries = 200

type PowerHandler func(deviceID string, on bool)

type LogLevel string

const (
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
	LogEvent LogLevel = "event"
)

type LogEntry struct {
	Time    int64    `json:"time"`
	Level   LogLevel `json:"level"`
	Message string   `json:"message"`
}

type TopicBinding struct {
	Topic    string `json:"topic"`
	DeviceID string `json:"deviceId"`
}

type Status struct {
	Enabled     bool           `json:"enabled"`
	Connected   bool           `json:"connected"`
	UIDSet      bool           `json:"uidSet"`
	UIDMasked   string         `json:"uidMasked,omitempty"`
	Started     bool           `json:"started"`
	TopicCount  int            `json:"topicCount"`
	Topics      []TopicBinding `json:"topics"`
	LastError   string         `json:"lastError,omitempty"`
	ConnectedAt int64          `json:"connectedAt,omitempty"`
	UpdatedAt   int64          `json:"updatedAt"`
}

type Manager struct {
	mu          sync.Mutex
	uid         string
	dev         *bemfadevice.Device
	topics      map[string]string // topic -> deviceID
	handler     PowerHandler
	started     bool
	logs        []LogEntry
	lastError   string
	connectedAt int64
}

var topicPattern = regexp.MustCompile(`^[a-zA-Z0-9]{2,29}001$`)

func NewManager(handler PowerHandler) *Manager {
	m := &Manager{
		topics:  map[string]string{},
		handler: handler,
		logs:    make([]LogEntry, 0, 64),
	}
	m.appendLogLocked(LogInfo, "bemfa manager ready")
	return m
}

func (m *Manager) Connected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dev != nil && m.dev.IsConnected()
}

func (m *Manager) UID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.uid
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	topics := make([]TopicBinding, 0, len(m.topics))
	for topic, id := range m.topics {
		topics = append(topics, TopicBinding{Topic: topic, DeviceID: id})
	}
	connected := m.dev != nil && m.dev.IsConnected()
	return Status{
		Enabled:     strings.TrimSpace(m.uid) != "",
		Connected:   connected,
		UIDSet:      strings.TrimSpace(m.uid) != "",
		UIDMasked:   maskUID(m.uid),
		Started:     m.started,
		TopicCount:  len(m.topics),
		Topics:      topics,
		LastError:   m.lastError,
		ConnectedAt: m.connectedAt,
		UpdatedAt:   time.Now().Unix(),
	}
}

func (m *Manager) Logs(limit int) []LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.logs) {
		limit = len(m.logs)
	}
	if limit == 0 {
		return []LogEntry{}
	}
	// return newest last for UI readability (chronological)
	start := len(m.logs) - limit
	out := make([]LogEntry, limit)
	copy(out, m.logs[start:])
	return out
}

func (m *Manager) ClearLogs() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = m.logs[:0]
	m.appendLogLocked(LogInfo, "logs cleared")
}

func (m *Manager) appendLog(level LogLevel, format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendLogLocked(level, format, args...)
}

func (m *Manager) appendLogLocked(level LogLevel, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	entry := LogEntry{
		Time:    time.Now().UnixMilli(),
		Level:   level,
		Message: msg,
	}
	m.logs = append(m.logs, entry)
	if len(m.logs) > maxLogEntries {
		// drop oldest
		overflow := len(m.logs) - maxLogEntries
		m.logs = append([]LogEntry(nil), m.logs[overflow:]...)
	}
	// also mirror to process log
	switch level {
	case LogError:
		log.Printf("[bemfa][error] %s", msg)
	case LogWarn:
		log.Printf("[bemfa][warn] %s", msg)
	case LogEvent:
		log.Printf("[bemfa][event] %s", msg)
	default:
		log.Printf("[bemfa] %s", msg)
	}
}

func (m *Manager) setLastErrorLocked(err error) {
	if err == nil {
		m.lastError = ""
		return
	}
	m.lastError = err.Error()
}

func (m *Manager) Reconfigure(uid string, devices []config.Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	uid = sanitizeUID(uid)
	if uid == m.uid && m.dev != nil {
		m.appendLogLocked(LogInfo, "resync subscriptions (%d devices)", len(devices))
		return m.syncLocked(devices)
	}
	m.closeLocked()
	m.uid = uid
	if uid == "" {
		m.appendLogLocked(LogInfo, "UID empty, mqtt disabled")
		m.setLastErrorLocked(nil)
		return nil
	}
	m.appendLogLocked(LogInfo, "starting mqtt with uid=%s", maskUID(uid))
	d := bemfadevice.NewDevice(uid)
	d.OnConnect(func() {
		m.mu.Lock()
		m.connectedAt = time.Now().Unix()
		m.setLastErrorLocked(nil)
		m.appendLogLocked(LogEvent, "mqtt connected")
		m.mu.Unlock()
	})
	d.OnDisconnect(func(err error) {
		m.mu.Lock()
		if err != nil {
			m.setLastErrorLocked(err)
			m.appendLogLocked(LogWarn, "mqtt disconnected: %v", err)
		} else {
			m.appendLogLocked(LogWarn, "mqtt disconnected")
		}
		m.mu.Unlock()
	})
	if err := d.Start(); err != nil {
		m.setLastErrorLocked(err)
		m.appendLogLocked(LogError, "mqtt start failed: %v", err)
		return fmt.Errorf("bemfa start: %w", err)
	}
	m.dev = d
	m.started = true
	m.appendLogLocked(LogInfo, "mqtt start requested")
	return m.syncLocked(devices)
}

func (m *Manager) EnsureDevice(d *config.Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !d.BemfaEnable {
		if d.BemfaTopic != "" {
			_ = m.unsubscribeLocked(d.BemfaTopic)
			m.appendLogLocked(LogInfo, "bemfa disabled for device=%s topic=%s", d.ID, d.BemfaTopic)
		}
		return nil
	}
	if m.uid == "" {
		err := errors.New("bemfa UID is empty; configure global UID first")
		m.setLastErrorLocked(err)
		m.appendLogLocked(LogError, "%v", err)
		return err
	}
	if m.dev == nil {
		err := errors.New("bemfa not connected")
		m.setLastErrorLocked(err)
		m.appendLogLocked(LogError, "%v", err)
		return err
	}
	topic := strings.TrimSpace(d.BemfaTopic)
	if topic == "" {
		topic = GenerateTopic()
		d.BemfaTopic = topic
		m.appendLogLocked(LogInfo, "generated topic=%s for device=%s", topic, d.ID)
	}
	topic = normalizeTopic(topic)
	d.BemfaTopic = topic
	if err := ValidateTopic(topic); err != nil {
		m.setLastErrorLocked(err)
		m.appendLogLocked(LogError, "invalid topic: %v", err)
		return err
	}
	name := strings.TrimSpace(d.BemfaName)
	if name == "" {
		name = strings.TrimSpace(d.Name)
	}
	name = sanitizeName(name)
	if name == "" {
		name = "WakeHub"
	}
	if err := createTopicIdempotent(m, m.uid, topic, name); err != nil {
		// 创建失败仍尝试订阅：主题可能已在控制台手动创建，或 API 误报参数错误。
		m.appendLogLocked(LogWarn, "create topic failed, try subscribe anyway: %v", err)
		if subErr := m.subscribeLocked(topic, d.ID); subErr != nil {
			m.setLastErrorLocked(err)
			m.appendLogLocked(LogError, "subscribe after create-fail also failed topic=%s: %v", topic, subErr)
			return fmt.Errorf("%w; subscribe: %v", err, subErr)
		}
		m.appendLogLocked(LogEvent, "subscribed existing/manual topic=%s device=%s (create API failed)", topic, d.ID)
		m.setLastErrorLocked(err) // keep last create error visible but device usable
		return nil
	}
	if err := m.subscribeLocked(topic, d.ID); err != nil {
		m.setLastErrorLocked(err)
		m.appendLogLocked(LogError, "subscribe failed topic=%s: %v", topic, err)
		return err
	}
	return nil
}

func (m *Manager) RemoveDevice(d config.Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.BemfaTopic != "" {
		_ = m.unsubscribeLocked(d.BemfaTopic)
		m.appendLogLocked(LogInfo, "unsubscribed topic=%s device=%s", d.BemfaTopic, d.ID)
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendLogLocked(LogInfo, "closing mqtt manager")
	m.closeLocked()
}

func (m *Manager) closeLocked() {
	if m.dev != nil {
		m.dev.Close()
		m.dev = nil
	}
	m.topics = map[string]string{}
	m.started = false
	m.connectedAt = 0
}

func (m *Manager) syncLocked(devices []config.Device) error {
	wanted := map[string]string{}
	for _, d := range devices {
		if !d.BemfaEnable || strings.TrimSpace(d.BemfaTopic) == "" {
			continue
		}
		wanted[d.BemfaTopic] = d.ID
	}
	for topic := range m.topics {
		if _, ok := wanted[topic]; !ok {
			_ = m.unsubscribeLocked(topic)
			m.appendLogLocked(LogInfo, "sync unsubscribe topic=%s", topic)
		}
	}
	for topic, id := range wanted {
		if err := m.subscribeLocked(topic, id); err != nil {
			m.appendLogLocked(LogWarn, "sync subscribe %s: %v", topic, err)
		}
	}
	m.appendLogLocked(LogInfo, "sync done topics=%d", len(m.topics))
	return nil
}

func (m *Manager) subscribeLocked(topic, deviceID string) error {
	if m.dev == nil {
		return errors.New("no device")
	}
	if old, ok := m.topics[topic]; ok && old == deviceID {
		return nil
	}
	if _, ok := m.topics[topic]; ok {
		_ = m.dev.Unsubscribe(topic)
		delete(m.topics, topic)
	}
	outlet := voice.NewOutlet(m.dev, topic)
	devID := deviceID
	if err := outlet.OnCommandWithReply(func(on bool) {
		action := "off"
		if on {
			action = "on"
		}
		m.appendLog(LogEvent, "command topic=%s device=%s action=%s", topic, devID, action)
		if m.handler != nil {
			m.handler(devID, on)
		}
	}); err != nil {
		return err
	}
	m.topics[topic] = deviceID
	m.appendLogLocked(LogEvent, "subscribed topic=%s device=%s", topic, deviceID)
	return nil
}

func (m *Manager) unsubscribeLocked(topic string) error {
	if m.dev == nil {
		delete(m.topics, topic)
		return nil
	}
	err := m.dev.Unsubscribe(topic)
	delete(m.topics, topic)
	return err
}

func createTopicIdempotent(m *Manager, uid, topic, name string) error {
	uid = sanitizeUID(uid)
	topic = normalizeTopic(topic)
	name = sanitizeName(name)
	if uid == "" {
		return errors.New("bemfa UID is empty")
	}
	if err := validateUID(uid); err != nil {
		return err
	}
	if err := ValidateTopic(topic); err != nil {
		return err
	}
	if name == "" {
		name = "WakeHub"
	}

	attempts := []struct {
		label string
		fn    func() error
	}{
		{
			label: "CreateTopic(v2,type=1)",
			fn: func() error {
				_, err := deviceapi.CreateTopic(deviceapi.CreateTopicRequest{
					UID:   uid,
					Topic: topic,
					Type:  int(deviceapi.ProtocolMQTT),
					Name:  name,
				})
				return err
			},
		},
		{
			label: "CreateTopic(v2,type=5,MQTTv2)",
			fn: func() error {
				_, err := deviceapi.CreateTopic(deviceapi.CreateTopicRequest{
					UID:   uid,
					Topic: topic,
					Type:  int(deviceapi.ProtocolMQTTV2),
					Name:  name,
				})
				return err
			},
		},
		{
			label: "CreateTopicV1(type=1)",
			fn: func() error {
				_, err := deviceapi.CreateTopicV1(deviceapi.CreateTopicRequest{
					UID:   uid,
					Topic: topic,
					Type:  int(deviceapi.ProtocolMQTT),
					Name:  name,
				})
				return err
			},
		},
		{
			label: "CreateTopics(v2,type=1)",
			fn: func() error {
				_, err := deviceapi.CreateTopics(deviceapi.CreateTopicsRequest{
					UID: uid,
					Topics: []deviceapi.TopicItem{{
						Topic: topic,
						Type:  int(deviceapi.ProtocolMQTT),
						Name:  name,
					}},
				})
				return err
			},
		},
	}

	var last error
	for _, a := range attempts {
		err := a.fn()
		if err == nil {
			m.appendLogLocked(LogEvent, "create topic ok via %s topic=%s name=%s", a.label, topic, name)
			return nil
		}
		if isAlreadyExists(err) {
			m.appendLogLocked(LogInfo, "topic already exists topic=%s", topic)
			return nil
		}
		last = err
		m.appendLogLocked(LogWarn, "create topic failed via %s topic=%s uid=%s: %v", a.label, topic, maskUID(uid), err)
		if !isParamError(err) {
			break
		}
	}

	return fmt.Errorf("%w | topic=%s name=%q uid=%s | 排查: 1)UID须为巴法「用户私钥」非密钥/AppID 2)主题仅字母数字且以001结尾 3)可先在MQTT控制台手动创建同名主题",
		last, topic, name, maskUID(uid))
}

func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *bemfaapi.APIError
	if errors.As(err, &apiErr) {
		msg := strings.ToLower(apiErr.Message)
		if strings.Contains(msg, "exist") || strings.Contains(msg, "已存在") || strings.Contains(msg, "重复") {
			return true
		}
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "exist") || strings.Contains(s, "已存在") || strings.Contains(s, "重复")
}

func isParamError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *bemfaapi.APIError
	if errors.As(err, &apiErr) {
		msg := strings.ToLower(apiErr.Message)
		if apiErr.Code == -1 || strings.Contains(msg, "参数") || strings.Contains(msg, "param") {
			return true
		}
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "参数") || strings.Contains(s, "param")
}

func ValidateTopic(topic string) error {
	topic = normalizeTopic(topic)
	if topic == "" {
		return errors.New("bemfa topic is empty")
	}
	if !strings.HasSuffix(topic, "001") {
		return fmt.Errorf("bemfa topic must end with 001 (outlet), got %s", topic)
	}
	if !topicPattern.MatchString(topic) {
		return fmt.Errorf("bemfa topic invalid %q: only letters/digits allowed, length 5-32, must end with 001", topic)
	}
	return nil
}

func normalizeTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	topic = strings.ToLower(topic)
	topic = strings.ReplaceAll(topic, ":", "")
	topic = strings.ReplaceAll(topic, "-", "")
	topic = strings.ReplaceAll(topic, "_", "")
	topic = strings.ReplaceAll(topic, " ", "")
	return topic
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 32 {
		r := []rune(name)
		name = string(r[:32])
	}
	return name
}

func maskUID(uid string) string {
	uid = sanitizeUID(uid)
	if len(uid) <= 6 {
		if uid == "" {
			return ""
		}
		return "****"
	}
	return uid[:3] + "****" + uid[len(uid)-3:]
}

// sanitizeUID strips spaces/newlines/zero-width chars common when pasting from console.
func sanitizeUID(uid string) string {
	uid = strings.TrimSpace(uid)
	uid = strings.ReplaceAll(uid, "\u200b", "")
	uid = strings.ReplaceAll(uid, "\u200c", "")
	uid = strings.ReplaceAll(uid, "\u200d", "")
	uid = strings.ReplaceAll(uid, "\ufeff", "")
	uid = strings.ReplaceAll(uid, "\r", "")
	uid = strings.ReplaceAll(uid, "\n", "")
	uid = strings.ReplaceAll(uid, " ", "")
	uid = strings.ReplaceAll(uid, "\t", "")
	return uid
}

func validateUID(uid string) error {
	uid = sanitizeUID(uid)
	if uid == "" {
		return errors.New("bemfa UID is empty")
	}
	// Private keys are typically long hex; short values are often AppID/wrong field.
	if len(uid) < 16 {
		return fmt.Errorf("bemfa UID too short (%d chars); 请使用控制台「用户私钥」(通常为较长十六进制)，不要用密钥/AppID", len(uid))
	}
	for _, r := range uid {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		// Allow non-hex private keys if platform ever changes, but warn via soft check only for pure invalid punctuation
		if r < 33 || r > 126 {
			return fmt.Errorf("bemfa UID contains invalid character")
		}
	}
	return nil
}

func GenerateTopic() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "pc" + hex.EncodeToString(b) + "001"
}

func IsOutletTopic(topic string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(topic)), "001")
}
