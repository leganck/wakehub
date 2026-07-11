package device

import (
	"fmt"
	"log"
	"strings"

	"github.com/leganck/wakehub/internal/bemfa"
	"github.com/leganck/wakehub/internal/clientlink"
	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/wol"
)

type View struct {
	config.Device
	ClientOnline   bool             `json:"clientOnline"`
	BemfaConnected bool             `json:"bemfaConnected"`
	BoundClientNICs []config.NICInfo `json:"boundClientNics,omitempty"`
	BoundHostname   string           `json:"boundHostname,omitempty"`
}

type Service struct {
	store *config.Store
	hub   *clientlink.Hub
	bemfa *bemfa.Manager
}

func NewService(store *config.Store, hub *clientlink.Hub) *Service {
	s := &Service{store: store, hub: hub}
	s.bemfa = bemfa.NewManager(s.onBemfaPower)
	return s
}

func (s *Service) Bemfa() *bemfa.Manager { return s.bemfa }

func (s *Service) StartBemfa() error {
	snap := s.store.Snapshot()
	return s.bemfa.Reconfigure(snap.Settings.BemfaUID, snap.Devices)
}

func (s *Service) onBemfaPower(deviceID string, on bool) {
	d, ok := s.store.GetDevice(deviceID)
	if !ok {
		log.Printf("bemfa power for unknown device %s", deviceID)
		return
	}
	if on {
		if err := s.Wake(d.ID); err != nil {
			log.Printf("bemfa wake %s: %v", d.Name, err)
		} else {
			log.Printf("bemfa wake ok: %s", d.Name)
		}
		return
	}
	if err := s.Shutdown(d.ID); err != nil {
		log.Printf("bemfa shutdown %s: %v", d.Name, err)
	} else {
		log.Printf("bemfa shutdown ok: %s", d.Name)
	}
}

func (s *Service) List() []View {
	list := s.store.ListDevices()
	out := make([]View, 0, len(list))
	bc := s.bemfa.Connected()
	for _, d := range list {
		v := View{Device: d, BemfaConnected: bc}
		if d.BoundClientKey != "" {
			if info, ok := s.hub.Get(d.BoundClientKey); ok {
				v.ClientOnline = true
				v.BoundClientNICs = info.NICs
				v.BoundHostname = info.Hostname
			}
		}
		out = append(out, v)
	}
	return out
}

func (s *Service) Get(id string) (View, bool) {
	d, ok := s.store.GetDevice(id)
	if !ok {
		return View{}, false
	}
	v := View{Device: d, BemfaConnected: s.bemfa.Connected()}
	if d.BoundClientKey != "" {
		if info, ok := s.hub.Get(d.BoundClientKey); ok {
			v.ClientOnline = true
			v.BoundClientNICs = info.NICs
			v.BoundHostname = info.Hostname
		}
	}
	return v, true
}

func (s *Service) Create(d config.Device) (config.Device, error) {
	d.ID = ""
	normalize(&d)
	if err := s.validateBemfa(&d); err != nil {
		return config.Device{}, err
	}
	saved, err := s.store.UpsertDevice(d)
	if err != nil {
		return config.Device{}, err
	}
	if err := s.bemfa.EnsureDevice(&saved); err != nil {
		_, _ = s.store.UpsertDevice(saved)
		return saved, fmt.Errorf("saved device but bemfa ensure failed: %w", err)
	}
	return s.store.UpsertDevice(saved)
}

func (s *Service) Update(id string, d config.Device) (config.Device, error) {
	old, ok := s.store.GetDevice(id)
	if !ok {
		return config.Device{}, fmt.Errorf("device not found")
	}
	d.ID = id
	if d.CreatedAt == 0 {
		d.CreatedAt = old.CreatedAt
	}
	normalize(&d)
	if err := s.validateBemfa(&d); err != nil {
		return config.Device{}, err
	}
	if old.BemfaTopic != "" && (old.BemfaTopic != d.BemfaTopic || !d.BemfaEnable) {
		s.bemfa.RemoveDevice(old)
	}
	saved, err := s.store.UpsertDevice(d)
	if err != nil {
		return config.Device{}, err
	}
	if err := s.bemfa.EnsureDevice(&saved); err != nil {
		return saved, fmt.Errorf("updated device but bemfa ensure failed: %w", err)
	}
	return s.store.UpsertDevice(saved)
}

func (s *Service) Delete(id string) error {
	d, ok := s.store.GetDevice(id)
	if ok {
		s.bemfa.RemoveDevice(d)
	}
	return s.store.DeleteDevice(id)
}

func (s *Service) Wake(id string) error {
	d, ok := s.store.GetDevice(id)
	if !ok {
		return fmt.Errorf("device not found")
	}
	return wol.Wake(d.MAC, d.Broadcast, d.Port, d.Repeat)
}

func (s *Service) Shutdown(id string) error {
	d, ok := s.store.GetDevice(id)
	if !ok {
		return fmt.Errorf("device not found")
	}
	if d.BoundClientKey == "" {
		return fmt.Errorf("device has no bound client")
	}
	return s.hub.Shutdown(d.BoundClientKey)
}

func (s *Service) FromClient(clientKey, mac, name string) (config.Device, error) {
	info, ok := s.hub.Get(clientKey)
	if !ok {
		return config.Device{}, fmt.Errorf("client not online")
	}
	if mac == "" {
		if len(info.NICs) > 0 {
			mac = info.NICs[0].MAC
		}
	}
	if mac == "" {
		return config.Device{}, fmt.Errorf("mac required")
	}
	broadcast := ""
	for _, n := range info.NICs {
		if strings.EqualFold(n.MAC, mac) && len(n.Broadcast) > 0 {
			broadcast = n.Broadcast[0]
			break
		}
	}
	if name == "" {
		name = info.Hostname
		if name == "" {
			name = clientKey
		}
	}
	for _, d := range s.store.ListDevices() {
		if d.BoundClientKey == clientKey || strings.EqualFold(d.MAC, mac) {
			d.Name = name
			d.MAC = mac
			d.Broadcast = broadcast
			d.BoundClientKey = clientKey
			return s.Update(d.ID, d)
		}
	}
	return s.Create(config.Device{
		Name:           name,
		MAC:            mac,
		Broadcast:      broadcast,
		Port:           9,
		Repeat:         3,
		BoundClientKey: clientKey,
	})
}

func (s *Service) OnSettingsChanged() error {
	snap := s.store.Snapshot()
	s.hub.SetToken(snap.Settings.ClientToken)
	return s.bemfa.Reconfigure(snap.Settings.BemfaUID, snap.Devices)
}

func normalize(d *config.Device) {
	d.Name = strings.TrimSpace(d.Name)
	d.MAC = strings.TrimSpace(d.MAC)
	d.Broadcast = strings.TrimSpace(d.Broadcast)
	d.BemfaTopic = strings.ToLower(strings.TrimSpace(d.BemfaTopic))
	d.BemfaName = strings.TrimSpace(d.BemfaName)
	d.BoundClientKey = strings.TrimSpace(d.BoundClientKey)
	if d.Port <= 0 {
		d.Port = 9
	}
	if d.Repeat <= 0 {
		d.Repeat = 3
	}
}

func (s *Service) validateBemfa(d *config.Device) error {
	if !d.BemfaEnable {
		return nil
	}
	if strings.TrimSpace(s.store.Settings().BemfaUID) == "" {
		return fmt.Errorf("enable bemfa requires global Bemfa UID in settings")
	}
	if d.BemfaTopic != "" {
		if err := bemfa.ValidateTopic(d.BemfaTopic); err != nil {
			return err
		}
	}
	return nil
}
