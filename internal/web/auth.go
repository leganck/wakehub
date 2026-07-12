package web

import (
	"net/http"

	"github.com/leganck/wakehub/internal/config"
)

// withBasicAuth wraps h with HTTP Basic authentication for Web/API.
// Client WebSocket path is exempt (uses client token instead).
func (s *Server) withBasicAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := s.store.Settings()
		wsPath := st.WSPath
		if wsPath == "" {
			wsPath = config.DefaultWSPath
		}
		// Exempt client WS and unauthenticated health probes.
		if r.URL.Path == wsPath || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			h.ServeHTTP(w, r)
			return
		}
		if !st.BasicAuthEnable {
			h.ServeHTTP(w, r)
			return
		}
		user := st.BasicAuthUser
		if user == "" {
			user = config.DefaultAuthUser
		}
		passHash := st.BasicAuthPassword
		u, p, ok := r.BasicAuth()
		if !ok || !secureEqual(u, user) || !config.CheckPassword(passHash, p) {
			w.Header().Set("WWW-Authenticate", `Basic realm="WakeHub", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
