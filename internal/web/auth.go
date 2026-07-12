package web

import (
	"crypto/subtle"
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
		// Exempt client WS endpoint
		if r.URL.Path == wsPath {
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
		pass := st.BasicAuthPassword
		if pass == "" {
			pass = config.DefaultAuthPassword
		}
		u, p, ok := r.BasicAuth()
		if !ok || !secureEqual(u, user) || !secureEqual(p, pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="WakeHub", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func secureEqual(a, b string) bool {
	// Constant-time compare; pad via equal length check first
	if len(a) != len(b) {
		// still compare against self to reduce timing signal on length
		subtle.ConstantTimeCompare([]byte(a), []byte(a))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
