package server

import (
	"crypto/subtle"
	"net/http"
)

// HandleDashboard serves the embedded HTML dashboard UI.
func (s *Server) HandleDashboard(w http.ResponseWriter, _ *http.Request) {
	if s.DashboardHTML == nil {
		http.Error(w, "Dashboard not configured", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.DashboardHTML)
}

// dashboardAuth is middleware that enforces HTTP Basic Auth using constant-time comparison
// to prevent timing attacks on the dashboard password.
func (s *Server) dashboardAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		password := s.Config.DashboardPassword
		if password == "" {
			next.ServeHTTP(w, r)
			return
		}

		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="WSBro Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
