package web

import (
	"net/http"
	"runtime"
	"time"
)

// BuildInfo is set by cmd/server from ldflags.
type BuildInfo struct {
	Version   string
	BuildTime string
	StartedAt time.Time
}

func (s *Server) SetBuildInfo(bi BuildInfo) {
	if bi.StartedAt.IsZero() {
		bi.StartedAt = time.Now()
	}
	s.build = bi
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	// Ready when config store is available (always after boot).
	writeJSON(w, 200, map[string]any{
		"status":  "ready",
		"devices": len(s.store.ListDevices()),
		"clients": len(s.hub.List()),
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	ver := s.build.Version
	if ver == "" {
		ver = "dev"
	}
	writeJSON(w, 200, map[string]any{
		"version":   ver,
		"buildTime": s.build.BuildTime,
		"startedAt": s.build.StartedAt.UTC().Format(time.RFC3339),
		"go":        runtime.Version(),
		"os":        runtime.GOOS,
		"arch":      runtime.GOARCH,
		// Client update default release source (override via env on client).
		"releaseRepo": "leganck/wakehub",
	})
}
