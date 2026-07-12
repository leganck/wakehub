package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/leganck/wakehub/internal/clientlink"
	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/device"
	"github.com/leganck/wakehub/internal/mdns"
	"github.com/leganck/wakehub/internal/openwrt"
	"github.com/leganck/wakehub/web"
)

type Server struct {
	store   *config.Store
	devices *device.Service
	hub     *clientlink.Hub
	browser *mdns.Browser
	prober  ProbeRunner
	mux     *http.ServeMux
	build   BuildInfo
}

// ProbeRunner is optional online probe control.
type ProbeRunner interface {
	ProbeDevice(id string) (any, error)
}

func New(store *config.Store, devices *device.Service, hub *clientlink.Hub, browser *mdns.Browser) *Server {
	s := &Server{store: store, devices: devices, hub: hub, browser: browser, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) SetProber(p ProbeRunner) { s.prober = p }

func (s *Server) Handler() http.Handler {
	return s.withBasicAuth(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/readyz", s.handleReadyz)
	s.mux.HandleFunc("/api/version", s.handleVersion)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/settings", s.handleSettings)
	s.mux.HandleFunc("/api/devices", s.handleDevices)
	s.mux.HandleFunc("/api/devices/", s.handleDeviceSub)
	s.mux.HandleFunc("/api/clients", s.handleClients)
	s.mux.HandleFunc("/api/discover", s.handleDiscover)
	s.mux.HandleFunc("/api/mqtt", s.handleMQTT)
	s.mux.HandleFunc("/api/mqtt/logs", s.handleMQTTLogs)
	s.mux.HandleFunc("/api/groups", s.handleGroups)
	s.mux.HandleFunc("/api/groups/", s.handleGroupSub)
	s.mux.HandleFunc("/api/schedules", s.handleSchedules)
	s.mux.HandleFunc("/api/schedules/", s.handleScheduleSub)
	s.mux.HandleFunc("/api/batch", s.handleBatch)

	settings := s.store.Settings()
	wsPath := settings.WSPath
	if wsPath == "" {
		wsPath = config.DefaultWSPath
	}
	s.mux.HandleFunc(wsPath, s.hub.HandleWS)

	staticFS, err := fs.Sub(web.StaticFS, "static")
	if err == nil {
		fileServer := http.FileServer(http.FS(staticFS))
		s.mux.Handle("/", fileServer)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	st := s.store.Settings()
	mqtt := s.devices.Bemfa().Status()
	ver := s.build.Version
	if ver == "" {
		ver = "dev"
	}
	writeJSON(w, 200, map[string]any{
		"bemfaConnected": mqtt.Connected,
		"bemfaUIDSet":    strings.TrimSpace(st.BemfaUID) != "",
		"listen":         st.Listen,
		"clients":        len(s.hub.List()),
		"devices":        len(s.store.ListDevices()),
		"mqtt":           mqtt,
		"version":        ver,
		"buildTime":      s.build.BuildTime,
	})
}

func (s *Server) handleMQTT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, 200, s.devices.Bemfa().Status())
}

func (s *Server) handleMQTTLogs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		writeJSON(w, 200, map[string]any{
			"list":   s.devices.Bemfa().Logs(limit),
			"status": s.devices.Bemfa().Status(),
		})
	case http.MethodDelete:
		s.devices.Bemfa().ClearLogs()
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Auto-generate client token when empty (first run / cleared config).
		tokenGenerated := false
		if tok, gen, err := s.store.EnsureClientToken(); err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		} else if gen {
			tokenGenerated = true
			s.hub.SetToken(tok)
		}
		st := s.store.Settings()
		out := st.Public()
		out["tokenGenerated"] = tokenGenerated
		writeJSON(w, 200, out)
	case http.MethodPut:
		cur := s.store.Settings()
		if !cur.GlobalsWritable() {
			writeJSON(w, 403, map[string]any{
				"error": "全局设置由 LuCI/UCI 管理，Web 端为只读。请在「服务 → WakeHub」修改，或将 web_global_mode 设为 writeback。",
			})
			return
		}
		var body struct {
			config.Settings
			RegenerateToken bool `json:"regenerateToken"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		err := s.store.UpdateSettings(func(st *config.Settings) error {
			// Preserve LuCI management flags (not editable from body blindly).
			managed := st.GlobalManagedByLuci
			mode := st.WebGlobalMode

			st.Listen = body.Listen
			st.BemfaUID = strings.TrimSpace(body.BemfaUID)
			if body.RegenerateToken || strings.TrimSpace(body.ClientToken) == "" {
				st.ClientToken = config.RandomToken(32)
			} else {
				st.ClientToken = body.ClientToken
			}
			if body.WSPath != "" {
				st.WSPath = body.WSPath
			}
			st.BasicAuthEnable = body.BasicAuthEnable
			st.BasicAuthUser = strings.TrimSpace(body.BasicAuthUser)
			// Empty password means "keep existing" (UI never echoes password back).
			if pw := strings.TrimSpace(body.BasicAuthPassword); pw != "" {
				st.BasicAuthPassword = pw
			}
			st.NotifyWebhook = strings.TrimSpace(body.NotifyWebhook)
			st.NotifyOnWake = body.NotifyOnWake
			st.NotifyOnShutdown = body.NotifyOnShutdown
			st.NotifyOnProbeChange = body.NotifyOnProbeChange
			st.NotifyOnSchedule = body.NotifyOnSchedule
			st.GlobalManagedByLuci = managed
			st.WebGlobalMode = mode
			return nil
		})
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		st := s.store.Settings()
		// OpenWrt writeback: mirror globals into UCI so LuCI stays source of truth on disk.
		if st.GlobalManagedByLuci && st.GlobalsWritable() {
			if err := openwrt.WriteGlobals(st); err != nil {
				writeJSON(w, 200, map[string]any{
					"settings": st.Public(),
					"warning":  "已写入 config.json，但同步 UCI 失败: " + err.Error(),
				})
				return
			}
		}
		pub := st.Public()
		if err := s.devices.OnSettingsChanged(); err != nil {
			writeJSON(w, 200, map[string]any{"settings": pub, "warning": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"settings": pub})
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"list": s.devices.List()})
	case http.MethodPost:
		var d config.Device
		if err := readJSON(r, &d); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		saved, err := s.devices.Create(d)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error(), "device": saved})
			return
		}
		writeJSON(w, 200, map[string]any{"device": saved})
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleDeviceSub(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	id := parts[0]
	if id == "from-client" {
		s.handleFromClient(w, r)
		return
	}
	if id == "batch" {
		s.handleBatch(w, r)
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			v, ok := s.devices.Get(id)
			if !ok {
				writeJSON(w, 404, map[string]any{"error": "not found"})
				return
			}
			writeJSON(w, 200, v)
		case http.MethodPut:
			var d config.Device
			if err := readJSON(r, &d); err != nil {
				writeJSON(w, 400, map[string]any{"error": err.Error()})
				return
			}
			saved, err := s.devices.Update(id, d)
			if err != nil {
				writeJSON(w, 400, map[string]any{"error": err.Error(), "device": saved})
				return
			}
			writeJSON(w, 200, map[string]any{"device": saved})
		case http.MethodDelete:
			if err := s.devices.Delete(id); err != nil {
				writeJSON(w, 400, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true})
		default:
			writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "wake":
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"error": "method not allowed"})
			return
		}
		if err := s.devices.Wake(id); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case "shutdown":
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"error": "method not allowed"})
			return
		}
		if err := s.devices.Shutdown(id); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case "probe":
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"error": "method not allowed"})
			return
		}
		if s.prober == nil {
			writeJSON(w, 503, map[string]any{"error": "probe runner not ready"})
			return
		}
		res, err := s.prober.ProbeDevice(id)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"probe": res})
	default:
		writeJSON(w, 404, map[string]any{"error": "not found"})
	}
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, 200, map[string]any{"list": s.hub.List()})
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	peers := []mdns.Peer{}
	if s.browser != nil {
		peers = s.browser.List()
	}
	writeJSON(w, 200, map[string]any{
		"clients": s.hub.List(),
		"mdns":    peers,
	})
}

func (s *Server) handleFromClient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	var body struct {
		ClientKey string `json:"clientKey"`
		MAC       string `json:"mac"`
		Name      string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	d, err := s.devices.FromClient(body.ClientKey, body.MAC, body.Name)
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"device": d})
}
