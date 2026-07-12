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
	CreatedAt      int64  `json:"createdAt,omitempty"`
	UpdatedAt      int64  `json:"updatedAt,omitempty"`
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
}

// CurrentConfigVersion is written into config files after migrations.
const CurrentConfigVersion = 1

type File struct {
	Version  int      `json:"version,omitempty"`
	Settings Settings `json:"settings"`
	Devices  []Device `json:"devices"`
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
		Devices: []Device{},
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
	for i := range s.data.Devices {
		if s.data.Devices[i].Port <= 0 || s.data.Devices[i].Port > 65535 {
			s.data.Devices[i].Port = 9
		}
		if s.data.Devices[i].Repeat <= 0 || s.data.Devices[i].Repeat > 20 {
			s.data.Devices[i].Repeat = 3
		}
	}
}

func (s *Store) Path() string { return s.path }

func (s *Store) Snapshot() File {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.data
	out.Devices = append([]Device(nil), s.data.Devices...)
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
