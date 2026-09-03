package webui

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/hostpack/hostpack/internal/config"
	hpruntime "github.com/hostpack/hostpack/internal/runtime"
)

type StateProvider interface {
	State() hpruntime.State
	Touch()
}

type Server struct {
	cfg      *config.Config
	state    StateProvider
	logs     *LogBuffer
	username string
	password string
}

type packStatus struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	Provider        string `json:"provider"`
	Java            int    `json:"java"`
	HeapMB          int    `json:"heapMb"`
	MachineMemoryMB int    `json:"machineMemoryMb"`
	MachineCPUs     int    `json:"machineCpus"`
}

type statusResponse struct {
	State  hpruntime.State `json:"state"`
	Packs  []packStatus    `json:"packs"`
	Domain string          `json:"domain"`
	Now    time.Time       `json:"now"`
}

func New(cfg *config.Config, state StateProvider, logs *LogBuffer, username, password string) *Server {
	return &Server{cfg: cfg, state: state, logs: logs, username: username, password: password}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/guest-status", s.guestStatus)
	mux.HandleFunc("GET /", s.page)
	mux.HandleFunc("GET /assets/app.css", s.styles)
	mux.HandleFunc("GET /assets/app.js", s.script)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/logs", s.recentLogs)
	return securityHeaders(s.authenticate(mux))
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/api/guest-status" {
			next.ServeHTTP(w, r)
			return
		}
		username, password, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.username)) == 1
		passwordOK := subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1
		if !ok || !userOK || !passwordOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="Hostpack", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		s.state.Touch()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) guestStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, PublicStatus(s.cfg, s.state.State()))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self'; style-src 'self'; script-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(pageHTML))
}

func (s *Server) styles(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(appCSS))
}

func (s *Server) script(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(appJS))
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	ids := make([]string, 0, len(s.cfg.Packs))
	for id := range s.cfg.Packs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	packs := make([]packStatus, 0, len(ids))
	for _, id := range ids {
		pack := s.cfg.Packs[id]
		packs = append(packs, packStatus{ID: id, DisplayName: pack.DisplayName, Provider: pack.Provider, Java: pack.Java, HeapMB: pack.MemoryMB, MachineMemoryMB: pack.MachineMemoryMB, MachineCPUs: pack.MachineCPUs})
	}
	writeJSON(w, statusResponse{State: s.state.State(), Packs: packs, Domain: s.cfg.Domain, Now: time.Now().UTC()})
}

func (s *Server) recentLogs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	writeJSON(w, struct {
		Logs []string `json:"logs"`
	}{Logs: s.logs.Recent(limit)})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}
