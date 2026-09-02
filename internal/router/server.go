package router

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hostpack/hostpack/internal/config"
	"github.com/hostpack/hostpack/internal/mcproto"
	hpruntime "github.com/hostpack/hostpack/internal/runtime"
)

type Server struct {
	cfg     *config.Config
	manager *hpruntime.Manager
	log     *slog.Logger
	sem     chan struct{}
	limiter *ipLimiter
}

func New(cfg *config.Config, m *hpruntime.Manager, log *slog.Logger) *Server {
	return &Server{cfg: cfg, manager: m, log: log, sem: make(chan struct{}, cfg.Runtime.MaxConnections), limiter: newIPLimiter(cfg.Runtime.ConnectionsPerMinute)}
}
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		select {
		case <-ctx.Done():
			_ = ln.Close()
		case <-s.manager.Done():
			_ = ln.Close()
		}
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-s.manager.Done():
				return nil
			default:
				return err
			}
		}
		select {
		case s.sem <- struct{}{}:
			go func() { defer func() { <-s.sem }(); s.handle(c) }()
		default:
			_ = c.Close()
		}
	}
}
func (s *Server) handle(c net.Conn) {
	defer c.Close()
	host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	if !s.limiter.allow(host) {
		return
	}
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(c)
	hs, err := mcproto.ReadHandshake(reader)
	if err != nil {
		s.log.Warn("bad handshake", "remote", host, "error", err)
		return
	}
	id, pack, ok := s.cfg.PackForHost(hs.Host)
	if hs.NextState == 1 {
		motd := "Unknown hostpack"
		if ok {
			st := s.manager.State()
			switch {
			case st.ActiveID == id && st.Phase == hpruntime.Ready:
				motd = pack.DisplayName + " is running"
			case st.ActiveID == id:
				motd = pack.DisplayName + " is " + string(st.Phase)
			case st.ActiveID != "":
				motd = pack.DisplayName + " is sleeping; " + st.ActiveID + " is active"
			default:
				motd = pack.DisplayName + " is sleeping — join to start"
			}
		}
		_ = mcproto.WriteStatus(c, reader, hs.Protocol, "Hostpack MVP", motd, 0, 0)
		return
	}
	if !ok {
		_ = mcproto.WriteLoginDisconnect(c, "Unknown hostpack hostname.")
		return
	}
	ready, msg := s.manager.Ensure(id)
	if !ready {
		_ = mcproto.WriteLoginDisconnect(c, msg)
		return
	}
	backend, err := net.DialTimeout("tcp", s.cfg.Runtime.BackendAddress, 5*time.Second)
	if err != nil {
		_ = mcproto.WriteLoginDisconnect(c, "Server became unavailable. Please reconnect.")
		return
	}
	defer backend.Close()
	_ = c.SetDeadline(time.Time{})
	if _, err = backend.Write(hs.Frame); err != nil {
		return
	}
	errc := make(chan error, 2)
	go func() { _, e := io.Copy(backend, reader); errc <- e }()
	go func() { _, e := io.Copy(c, backend); errc <- e }()
	<-errc
}

type ipBucket struct {
	start time.Time
	count int
}
type ipLimiter struct {
	mu    sync.Mutex
	limit int
	ips   map[string]ipBucket
}

func newIPLimiter(limit int) *ipLimiter { return &ipLimiter{limit: limit, ips: map[string]ipBucket{}} }
func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.ips[ip]
	if now.Sub(b.start) >= time.Minute {
		b = ipBucket{start: now}
	}
	if b.start.IsZero() {
		b.start = now
	}
	b.count++
	l.ips[ip] = b
	for k, v := range l.ips {
		if now.Sub(v.start) > 2*time.Minute {
			delete(l.ips, k)
		}
	}
	return b.count <= l.limit
}
