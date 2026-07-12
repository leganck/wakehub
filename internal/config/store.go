package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const DefaultListen = ":8080"
const DefaultWSPath = "/api/ws/client"
const DefaultAuthUser = "admin"
const DefaultAuthPassword = "admin"

type NICInfo struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac"`
	IPv4      []string `json:"ipv4,omitempty"`
	Broadcast []string `json:"broadcast,omitempty"`
}

type Device struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	MAC            string `json:"mac"`
	Broadcast      string `json:"broadcast,omitempty"`
	Port           int    `json:"port"`
	Repeat         int    `json:"repeat"`
	BemfaEnable    bool   `json:"bemfaEnable"`
	BemfaTopic     string `json:"bemfaTopic,omitempty"`
	BemfaName      string `json:"bemfaName,omitempty"`
	BoundClientKey string `json:"boundClientKey,omitempty"`
	// PreferredNIC is the interface name hint when picking MAC from multi-NIC clients.
	PreferredNIC string `json:"preferredNic,omitempty"`
	// Online probe (tcp/icmp). Host empty => use first IPv4 of bound client or derived.
	ProbeMethod   string `json:"probeMethod,omitempty"` // off|tcp|icmp
	ProbeHost     string `json:"probeHost,omitempty"`
	ProbePort     int    `json:"probePort,omitempty"` // tcp port, default 3389
	ProbeInterval int    `json:"probeInterval,omitempty"` // seconds, default 60
	CreatedAt     int64  `json:"createdAt,omitempty"`
	UpdatedAt     int64  `json:"updatedAt,omitempty"`
}

// Group is a named set of devices for batch wake/shutdown.
type Group struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	DeviceIDs []string `json:"deviceIds"`
	CreatedAt int64    `json:"createdAt,omitempty"`
	UpdatedAt int64    `json:"updatedAt,omitempty"`
}

// Schedule runs wake/shutdown at a local time on selected weekdays.
type Schedule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Action     string `json:"action"` // wake|shutdown
	DeviceID   string `json:"deviceId,omitempty"`
	GroupID    string `json:"groupId,omitempty"`
	Hour       int    `json:"hour"`   // 0-23
	Minute     int    `json:"minute"` // 0-59
	Weekdays   []int  `json:"weekdays,omitempty"` // 0=Sun .. 6=Sat; empty = every day
	LastRunDay string `json:"lastRunDay,omitempty"` // YYYY-MM-DD
	CreatedAt  int64  `json:"createdAt,omitempty"`
	UpdatedAt  int64  `json:"updatedAt,omitempty"`
}

// WebGlobalMode values when globals are managed outside plain config.json (e.g. OpenWrt LuCI).
const (
	WebGlobalModeReadonly  = "readonly"  // Web UI cannot change globals
	WebGlobalModeWriteback = "writeback" // Web UI may change globals and sync back to UCI
)

type Settings struct {
	Listen            string `json:"listen"`
	BemfaUID          string `json:"bemfaUID"`
	ClientToken       string `json:"clientToken"`
	WSPath            string `json:"wsPath"`
	BasicAuthEnable   bool   `json:"basicAuthEnable"`
	BasicAuthUser     string `json:"basicAuthUser"`
	BasicAuthPassword string `json:"basicAuthPassword"`
	// GlobalManagedByLuci marks OpenWrt installs where UCI/LuCI owns global settings.
	GlobalManagedByLuci bool `json:"globalManagedByLuci"`
	// WebGlobalMode is "readonly" (default when managed) or "writeback".
	WebGlobalMode string `json:"webGlobalMode,omitempty"`
	// Notify (webhook)
	NotifyWebhook        string `json:"notifyWebhook,omitempty"`
	NotifyOnWake         bool   `json:"notifyOnWake"`
	NotifyOnShutdown     bool   `json:"notifyOnShutdown"`
	NotifyOnProbeChange  bool   `json:"notifyOnProbeChange"`
	NotifyOnSchedule     bool   `json:"notifyOnSchedule"`
}

// CurrentConfigVersion is written into config files after migrations.
const CurrentConfigVersion = 1

type File struct {
	Version   int        `json:"version,omitempty"`
	Settings  Settings   `json:"settings"`
	Devices   []Device   `json:"devices"`
	Groups    []Group    `json:"groups,omitempty"`
	Schedules []Schedule `json:"schedules,omitempty"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data File
}

func DefaultFile() File {
	return File{
		Version: CurrentConfigVersion,
		Settings: Settings{
			Listen:            DefaultListen,
			WSPath:            DefaultWSPath,
			BasicAuthEnable:   true,
			BasicAuthUser:     DefaultAuthUser,
			BasicAuthPassword: DefaultAuthPassword,
		},
		Devices:   []Device{},
		Groups:    []Group{},
		Schedules: []Schedule{},
	}
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = filepath.Join("data", "config.json")
	}
	s := &Store{path: path, data: DefaultFile()}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := s.Save(); err != nil {
				return nil, err
			}
			return s, nil
		}
		return nil, err
	}
	migrated := false
	if len(b) > 0 {
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if needsBasicAuthMigration(b) {
			// Legacy configs without basicAuthEnable were effectively open.
			// Enable Basic auth with default admin/admin and bump version.
			s.data.Settings.BasicAuthEnable = true
			if strings.TrimSpace(s.data.Settings.BasicAuthUser) == "" {
				s.data.Settings.BasicAuthUser = DefaultAuthUser
			}
			if strings.TrimSpace(s.data.Settings.BasicAuthPassword) == "" {
				s.data.Settings.BasicAuthPassword = DefaultAuthPassword
			}
			migrated = true
		}
	}
	s.normalize()
	if s.data.Version < CurrentConfigVersion {
		s.data.Version = CurrentConfigVersion
		migrated = true
	}
	if migrated {
		_ = backupConfigFile(path)
		if err := s.Save(); err != nil {
			return nil, fmt.Errorf("migrate config: %w", err)
		}
	}
	return s, nil
}

// needsBasicAuthMigration is true when settings JSON has no basicAuthEnable key
// (pre-auth releases). Empty files / missing settings also migrate.
func needsBasicAuthMigration(raw []byte) bool {
	var top struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return false
	}
	if top.Settings == nil {
		return true
	}
	_, ok := top.Settings["basicAuthEnable"]
	return !ok
}

func backupConfigFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".bak", b, 0o644)
}

func (s *Store) normalize() {
	if s.data.Settings.Listen == "" {
		s.data.Settings.Listen = DefaultListen
	}
	if s.data.Settings.WSPath == "" {
		s.data.Settings.WSPath = DefaultWSPath
	}
	if strings.TrimSpace(s.data.Settings.BasicAuthUser) == "" {
		s.data.Settings.BasicAuthUser = DefaultAuthUser
	}
	if strings.TrimSpace(s.data.Settings.BasicAuthPassword) == "" {
		s.data.Settings.BasicAuthPassword = DefaultAuthPassword
	}
	if s.data.Devices == nil {
		s.data.Devices = []Device{}
	}
	if s.data.Groups == nil {
		s.data.Groups = []Group{}
	}
	if s.data.Schedules == nil {
		s.data.Schedules = []Schedule{}
	}
	for i := range s.data.Devices {
		normalizeDeviceFields(&s.data.Devices[i])
	}
	for i := range s.data.Schedules {
		normalizeScheduleFields(&s.data.Schedules[i])
	}
}

func normalizeDeviceFields(d *Device) {
	if d.Port <= 0 || d.Port > 65535 {
		d.Port = 9
	}
	if d.Repeat <= 0 || d.Repeat > 20 {
		d.Repeat = 3
	}
	d.ProbeMethod = strings.ToLower(strings.TrimSpace(d.ProbeMethod))
	if d.ProbeMethod == "" {
		d.ProbeMethod = "off"
	}
	if d.ProbeMethod == "tcp" && (d.ProbePort <= 0 || d.ProbePort > 65535) {
		d.ProbePort = 3389
	}
	if d.ProbeInterval <= 0 {
		d.ProbeInterval = 60
	}
	if d.ProbeInterval < 15 {
		d.ProbeInterval = 15
	}
}

func normalizeScheduleFields(sc *Schedule) {
	sc.Action = strings.ToLower(strings.TrimSpace(sc.Action))
	if sc.Hour < 0 || sc.Hour > 23 {
		sc.Hour = 0
	}
	if sc.Minute < 0 || sc.Minute > 59 {
		sc.Minute = 0
	}
}

func (s *Store) Path() string { return s.path }

func (s *Store) Snapshot() File {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.data
	out.Devices = append([]Device(nil), s.data.Devices...)
	out.Groups = append([]Group(nil), s.data.Groups...)
	out.Schedules = append([]Schedule(nil), s.data.Schedules...)
	for i := range out.Groups {
		out.Groups[i].DeviceIDs = append([]string(nil), s.data.Groups[i].DeviceIDs...)
	}
	for i := range out.Schedules {
		out.Schedules[i].Weekdays = append([]int(nil), s.data.Schedules[i].Weekdays...)
	}
	return out
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Settings
}

func (s *Store) UpdateSettings(fn func(*Settings) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.data.Settings); err != nil {
		return err
	}
	if s.data.Settings.Listen == "" {
		s.data.Settings.Listen = DefaultListen
	}
	if s.data.Settings.WSPath == "" {
		s.data.Settings.WSPath = DefaultWSPath
	}
	if strings.TrimSpace(s.data.Settings.BasicAuthUser) == "" {
		s.data.Settings.BasicAuthUser = DefaultAuthUser
	}
	// Do not force-default password here: empty may mean "leave unchanged" on API update.
	// Missing password is filled in normalize() after load / migration.
	if strings.TrimSpace(s.data.Settings.BasicAuthPassword) == "" {
		s.data.Settings.BasicAuthPassword = DefaultAuthPassword
	}
	return s.saveLocked()
}

// GlobalsWritable reports whether the Web API may modify global settings.
// When GlobalManagedByLuci is set, only WebGlobalModeWriteback allows writes.
func (st Settings) GlobalsWritable() bool {
	if !st.GlobalManagedByLuci {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(st.WebGlobalMode), WebGlobalModeWriteback)
}

// PublicSettings returns settings safe for API responses (password never included).
func (st Settings) Public() map[string]any {
	mode := strings.TrimSpace(st.WebGlobalMode)
	if st.GlobalManagedByLuci && mode == "" {
		mode = WebGlobalModeReadonly
	}
	return map[string]any{
		"listen":                 st.Listen,
		"bemfaUID":               st.BemfaUID,
		"clientToken":            st.ClientToken,
		"wsPath":                 st.WSPath,
		"basicAuthEnable":        st.BasicAuthEnable,
		"basicAuthUser":          st.BasicAuthUser,
		"basicAuthPasswordSet":   strings.TrimSpace(st.BasicAuthPassword) != "",
		"globalManagedByLuci":    st.GlobalManagedByLuci,
		"webGlobalMode":          mode,
		"globalSettingsWritable": st.GlobalsWritable(),
		"notifyWebhook":          st.NotifyWebhook,
		"notifyOnWake":           st.NotifyOnWake,
		"notifyOnShutdown":       st.NotifyOnShutdown,
		"notifyOnProbeChange":    st.NotifyOnProbeChange,
		"notifyOnSchedule":       st.NotifyOnSchedule,
	}
}

// ListenPort extracts the TCP port from Listen (e.g. ":8080" -> "8080").
func (st Settings) ListenPort() string {
	s := strings.TrimSpace(st.Listen)
	if s == "" {
		return "8080"
	}
	if i := strings.LastIndex(s, ":"); i >= 0 && i+1 < len(s) {
		p := s[i+1:]
		if p != "" {
			return p
		}
	}
	if strings.Trim(s, "0123456789") == "" {
		return s
	}
	return "8080"
}

// EnsureClientToken generates and persists a random client token when empty.
// Returns (token, newlyGenerated, error).
func (s *Store) EnsureClientToken() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.data.Settings.ClientToken) != "" {
		return s.data.Settings.ClientToken, false, nil
	}
	tok := RandomToken(32)
	s.data.Settings.ClientToken = tok
	if err := s.saveLocked(); err != nil {
		return "", false, err
	}
	return tok, true, nil
}

func (s *Store) ListDevices() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Device(nil), s.data.Devices...)
}

func (s *Store) GetDevice(id string) (Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.data.Devices {
		if d.ID == id {
			return d, true
		}
	}
	return Device{}, false
}

func (s *Store) UpsertDevice(d Device) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if d.Port <= 0 || d.Port > 65535 {
		d.Port = 9
	}
	if d.Repeat <= 0 || d.Repeat > 20 {
		d.Repeat = 3
	}
	if d.Name == "" {
		return Device{}, errors.New("name is required")
	}
	if d.MAC == "" {
		return Device{}, errors.New("mac is required")
	}
	normalizeDeviceFields(&d)
	idx := -1
	for i := range s.data.Devices {
		if s.data.Devices[i].ID == d.ID && d.ID != "" {
			idx = i
			break
		}
	}
	if idx < 0 {
		if d.ID == "" {
			d.ID = randomID(10)
		}
		d.CreatedAt = now
		d.UpdatedAt = now
		s.data.Devices = append(s.data.Devices, d)
	} else {
		d.CreatedAt = s.data.Devices[idx].CreatedAt
		if d.CreatedAt == 0 {
			d.CreatedAt = now
		}
		d.UpdatedAt = now
		s.data.Devices[idx] = d
	}
	if err := s.saveLocked(); err != nil {
		return Device{}, err
	}
	return d, nil
}

func (s *Store) DeleteDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Devices[:0]
	found := false
	for _, d := range s.data.Devices {
		if d.ID == id {
			found = true
			continue
		}
		out = append(out, d)
	}
	if !found {
		return fmt.Errorf("device %s not found", id)
	}
	s.data.Devices = out
	// drop from groups
	for i := range s.data.Groups {
		ids := s.data.Groups[i].DeviceIDs[:0]
		for _, did := range s.data.Groups[i].DeviceIDs {
			if did != id {
				ids = append(ids, did)
			}
		}
		s.data.Groups[i].DeviceIDs = ids
	}
	return s.saveLocked()
}

func (s *Store) ListGroups() []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Group, len(s.data.Groups))
	for i, g := range s.data.Groups {
		out[i] = g
		out[i].DeviceIDs = append([]string(nil), g.DeviceIDs...)
	}
	return out
}

func (s *Store) GetGroup(id string) (Group, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.data.Groups {
		if g.ID == id {
			g.DeviceIDs = append([]string(nil), g.DeviceIDs...)
			return g, true
		}
	}
	return Group{}, false
}

func (s *Store) UpsertGroup(g Group) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		return Group{}, errors.New("name is required")
	}
	if g.DeviceIDs == nil {
		g.DeviceIDs = []string{}
	}
	now := time.Now().Unix()
	idx := -1
	for i := range s.data.Groups {
		if s.data.Groups[i].ID == g.ID && g.ID != "" {
			idx = i
			break
		}
	}
	if idx < 0 {
		if g.ID == "" {
			g.ID = randomID(10)
		}
		g.CreatedAt = now
		g.UpdatedAt = now
		s.data.Groups = append(s.data.Groups, g)
	} else {
		g.CreatedAt = s.data.Groups[idx].CreatedAt
		g.UpdatedAt = now
		s.data.Groups[idx] = g
	}
	if err := s.saveLocked(); err != nil {
		return Group{}, err
	}
	return g, nil
}

func (s *Store) DeleteGroup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Groups[:0]
	found := false
	for _, g := range s.data.Groups {
		if g.ID == id {
			found = true
			continue
		}
		out = append(out, g)
	}
	if !found {
		return fmt.Errorf("group %s not found", id)
	}
	s.data.Groups = out
	return s.saveLocked()
}

func (s *Store) ListSchedules() []Schedule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Schedule, len(s.data.Schedules))
	for i, sc := range s.data.Schedules {
		out[i] = sc
		out[i].Weekdays = append([]int(nil), sc.Weekdays...)
	}
	return out
}

func (s *Store) GetSchedule(id string) (Schedule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.data.Schedules {
		if sc.ID == id {
			sc.Weekdays = append([]int(nil), sc.Weekdays...)
			return sc, true
		}
	}
	return Schedule{}, false
}

func (s *Store) UpsertSchedule(sc Schedule) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc.Name = strings.TrimSpace(sc.Name)
	if sc.Name == "" {
		return Schedule{}, errors.New("name is required")
	}
	sc.Action = strings.ToLower(strings.TrimSpace(sc.Action))
	if sc.Action != "wake" && sc.Action != "shutdown" {
		return Schedule{}, errors.New("action must be wake or shutdown")
	}
	if sc.DeviceID == "" && sc.GroupID == "" {
		return Schedule{}, errors.New("deviceId or groupId required")
	}
	normalizeScheduleFields(&sc)
	now := time.Now().Unix()
	idx := -1
	for i := range s.data.Schedules {
		if s.data.Schedules[i].ID == sc.ID && sc.ID != "" {
			idx = i
			break
		}
	}
	if idx < 0 {
		if sc.ID == "" {
			sc.ID = randomID(10)
		}
		sc.CreatedAt = now
		sc.UpdatedAt = now
		s.data.Schedules = append(s.data.Schedules, sc)
	} else {
		sc.CreatedAt = s.data.Schedules[idx].CreatedAt
		if sc.LastRunDay == "" {
			sc.LastRunDay = s.data.Schedules[idx].LastRunDay
		}
		sc.UpdatedAt = now
		s.data.Schedules[idx] = sc
	}
	if err := s.saveLocked(); err != nil {
		return Schedule{}, err
	}
	return sc, nil
}

func (s *Store) MarkScheduleRun(id, day string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Schedules {
		if s.data.Schedules[i].ID == id {
			s.data.Schedules[i].LastRunDay = day
			return s.saveLocked()
		}
	}
	return fmt.Errorf("schedule %s not found", id)
}

func (s *Store) DeleteSchedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Schedules[:0]
	found := false
	for _, sc := range s.data.Schedules {
		if sc.ID == id {
			found = true
			continue
		}
		out = append(out, sc)
	}
	if !found {
		return fmt.Errorf("schedule %s not found", id)
	}
	s.data.Schedules = out
	return s.saveLocked()
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
