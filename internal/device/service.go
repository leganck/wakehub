package device

import (
	"fmt"
	"log"
	"strings"

	"github.com/leganck/wakehub/internal/bemfa"
	"github.com/leganck/wakehub/internal/clientlink"
	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/notify"
	"github.com/leganck/wakehub/internal/probe"
	"github.com/leganck/wakehub/internal/wol"
)

type View struct {
	config.Device
	ClientOnline    bool             `json:"clientOnline"`
	BemfaConnected  bool             `json:"bemfaConnected"` // global MQTT session
	BoundClientNICs  []config.NICInfo `json:"boundClientNics,omitempty"`
	BoundHostname    string           `json:"boundHostname,omitempty"`
	ClientVersion   string           `json:"clientVersion,omitempty"`
	ClientLastEvent string           `json:"clientLastEvent,omitempty"`
	ClientLastError string           `json:"clientLastError,omitempty"`
	ClientLastEventAt int64          `json:"clientLastEventAt,omitempty"`
	ProbeOnline     *bool            `json:"probeOnline,omitempty"`
	Probe           *probe.Result    `json:"probe,omitempty"`
}

type Service struct {
	store  *config.Store
	hub    *clientlink.Hub
	bemfa  *bemfa.Manager
	probe  *probe.StatusCache
	notify *notify.Sender
}

func NewService(store *config.Store, hub *clientlink.Hub) *Service {
	s := &Service{store: store, hub: hub, probe: probe.NewStatusCache()}
	s.bemfa = bemfa.NewManager(s.onBemfaPower)
	s.notify = notify.New(store)
	return s
}

func (s *Service) Bemfa() *bemfa.Manager       { return s.bemfa }
func (s *Service) ProbeCache() *probe.StatusCache { return s.probe }
func (s *Service) Notify() *notify.Sender       { return s.notify }

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

func (s *Service) nicsForDevice(d config.Device) []config.NICInfo {
	if d.BoundClientKey == "" {
		return nil
	}
	if info, ok := s.hub.Get(d.BoundClientKey); ok {
		return info.NICs
	}
	return nil
}

func (s *Service) enrich(d config.Device) View {
	v := View{Device: d, BemfaConnected: s.bemfa.Connected()}
	if d.BoundClientKey != "" {
		if info, ok := s.hub.Get(d.BoundClientKey); ok {
			v.ClientOnline = true
			v.BoundClientNICs = info.NICs
			v.BoundHostname = info.Hostname
			v.ClientVersion = info.Version
			v.ClientLastEvent = info.LastEvent
			v.ClientLastError = info.LastError
			v.ClientLastEventAt = info.LastEventAt
		}
	}
	if pr, ok := s.probe.Get(d.ID); ok && pr.Method != "off" && pr.Error != "probe disabled" {
		online := pr.Online
		v.ProbeOnline = &online
		cp := pr
		v.Probe = &cp
	}
	return v
}

func (s *Service) List() []View {
	list := s.store.ListDevices()
	out := make([]View, 0, len(list))
	for _, d := range list {
		out = append(out, s.enrich(d))
	}
	return out
}

func (s *Service) Get(id string) (View, bool) {
	d, ok := s.store.GetDevice(id)
	if !ok {
		return View{}, false
	}
	return s.enrich(d), true
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
	err := wol.Wake(d.MAC, d.Broadcast, d.Port, d.Repeat)
	if s.notify != nil {
		s.notify.Send(notify.Event{
			Event: "wake", DeviceID: d.ID, DeviceName: d.Name,
			OK: err == nil, Error: errString(err),
		})
	}
	return err
}

func (s *Service) Shutdown(id string) error {
	d, ok := s.store.GetDevice(id)
	if !ok {
		return fmt.Errorf("device not found")
	}
	if d.BoundClientKey == "" {
		err := fmt.Errorf("device has no bound client")
		if s.notify != nil {
			s.notify.Send(notify.Event{
				Event: "shutdown", DeviceID: d.ID, DeviceName: d.Name,
				OK: false, Error: err.Error(),
			})
		}
		return err
	}
	err := s.hub.Shutdown(d.BoundClientKey)
	if s.notify != nil {
		s.notify.Send(notify.Event{
			Event: "shutdown", DeviceID: d.ID, DeviceName: d.Name,
			OK: err == nil, Error: errString(err),
		})
	}
	return err
}

func (s *Service) Batch(action string, ids []string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		var err error
		switch action {
		case "wake":
			err = s.Wake(id)
		case "shutdown":
			err = s.Shutdown(id)
		default:
			err = fmt.Errorf("unknown action")
		}
		if err != nil {
			out[id] = err.Error()
		} else {
			out[id] = "ok"
		}
	}
	return out
}

func (s *Service) WakeGroup(groupID string) error {
	g, ok := s.store.GetGroup(groupID)
	if !ok {
		return fmt.Errorf("group not found")
	}
	res := s.Batch("wake", g.DeviceIDs)
	for _, v := range res {
		if v != "ok" {
			return fmt.Errorf("partial failure: %v", res)
		}
	}
	return nil
}

func (s *Service) ShutdownGroup(groupID string) error {
	g, ok := s.store.GetGroup(groupID)
	if !ok {
		return fmt.Errorf("group not found")
	}
	res := s.Batch("shutdown", g.DeviceIDs)
	for _, v := range res {
		if v != "ok" {
			return fmt.Errorf("partial failure: %v", res)
		}
	}
	return nil
}

func (s *Service) OnProbeChange(prev, cur probe.Result) {
	if s.notify == nil {
		return
	}
	name := cur.DeviceID
	if d, ok := s.store.GetDevice(cur.DeviceID); ok {
		name = d.Name
	}
	detail := "offline"
	if cur.Online {
		detail = "online"
	}
	s.notify.Send(notify.Event{
		Event: "probe", DeviceID: cur.DeviceID, DeviceName: name,
		OK: cur.Online, Error: cur.Error, Detail: detail,
	})
}

func (s *Service) OnSchedule(action, targetType, targetID, targetName string, ok bool, errMsg string) {
	if s.notify == nil {
		return
	}
	ev := notify.Event{
		Event: "schedule", OK: ok, Error: errMsg, Detail: action + " " + targetType,
	}
	if targetType == "group" {
		ev.GroupID, ev.GroupName = targetID, targetName
	} else {
		ev.DeviceID, ev.DeviceName = targetID, targetName
	}
	s.notify.Send(ev)
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
	prefNIC := ""
	for _, n := range info.NICs {
		if strings.EqualFold(n.MAC, mac) {
			prefNIC = n.Name
			if len(n.Broadcast) > 0 {
				broadcast = n.Broadcast[0]
			}
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
			if prefNIC != "" {
				d.PreferredNIC = prefNIC
			}
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
		PreferredNIC:   prefNIC,
		ProbeMethod:    "off",
	})
}

func (s *Service) OnSettingsChanged() error {
	snap := s.store.Snapshot()
	s.hub.SetToken(snap.Settings.ClientToken)
	return s.bemfa.Reconfigure(snap.Settings.BemfaUID, snap.Devices)
}

func (s *Service) NICsForProbe(deviceID string) []config.NICInfo {
	d, ok := s.store.GetDevice(deviceID)
	if !ok {
		return nil
	}
	return s.nicsForDevice(d)
}

func normalize(d *config.Device) {
	d.Name = strings.TrimSpace(d.Name)
	d.MAC = strings.TrimSpace(d.MAC)
	d.Broadcast = strings.TrimSpace(d.Broadcast)
	d.BemfaTopic = strings.ToLower(strings.TrimSpace(d.BemfaTopic))
	d.BemfaName = strings.TrimSpace(d.BemfaName)
	d.BoundClientKey = strings.TrimSpace(d.BoundClientKey)
	d.PreferredNIC = strings.TrimSpace(d.PreferredNIC)
	d.ProbeHost = strings.TrimSpace(d.ProbeHost)
	d.ProbeMethod = strings.ToLower(strings.TrimSpace(d.ProbeMethod))
	if d.ProbeMethod == "" {
		d.ProbeMethod = "off"
	}
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

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
