package web

import (
	"net/http"
	"strings"

	"github.com/leganck/wakehub/internal/config"
)

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	var body struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	body.Action = strings.ToLower(strings.TrimSpace(body.Action))
	if body.Action != "wake" && body.Action != "shutdown" {
		writeJSON(w, 400, map[string]any{"error": "action must be wake or shutdown"})
		return
	}
	if len(body.IDs) == 0 {
		writeJSON(w, 400, map[string]any{"error": "ids required"})
		return
	}
	writeJSON(w, 200, map[string]any{"results": s.devices.Batch(body.Action, body.IDs)})
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"list": s.store.ListGroups()})
	case http.MethodPost:
		var g config.Group
		if err := readJSON(r, &g); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		g.ID = ""
		saved, err := s.store.UpsertGroup(g)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"group": saved})
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleGroupSub(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/groups/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			g, ok := s.store.GetGroup(id)
			if !ok {
				writeJSON(w, 404, map[string]any{"error": "not found"})
				return
			}
			writeJSON(w, 200, g)
		case http.MethodPut:
			var g config.Group
			if err := readJSON(r, &g); err != nil {
				writeJSON(w, 400, map[string]any{"error": err.Error()})
				return
			}
			g.ID = id
			saved, err := s.store.UpsertGroup(g)
			if err != nil {
				writeJSON(w, 400, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{"group": saved})
		case http.MethodDelete:
			if err := s.store.DeleteGroup(id); err != nil {
				writeJSON(w, 400, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true})
		default:
			writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		}
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	switch parts[1] {
	case "wake":
		if err := s.devices.WakeGroup(id); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case "shutdown":
		if err := s.devices.ShutdownGroup(id); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeJSON(w, 404, map[string]any{"error": "not found"})
	}
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"list": s.store.ListSchedules()})
	case http.MethodPost:
		var sc config.Schedule
		if err := readJSON(r, &sc); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		sc.ID = ""
		saved, err := s.store.UpsertSchedule(sc)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"schedule": saved})
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleScheduleSub(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/schedules/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	id := parts[0]
	switch r.Method {
	case http.MethodGet:
		sc, ok := s.store.GetSchedule(id)
		if !ok {
			writeJSON(w, 404, map[string]any{"error": "not found"})
			return
		}
		writeJSON(w, 200, sc)
	case http.MethodPut:
		var sc config.Schedule
		if err := readJSON(r, &sc); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		sc.ID = id
		saved, err := s.store.UpsertSchedule(sc)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"schedule": saved})
	case http.MethodDelete:
		if err := s.store.DeleteSchedule(id); err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}
