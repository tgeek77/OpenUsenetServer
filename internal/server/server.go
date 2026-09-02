package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openusenet/openusenet/internal/admin"
	"github.com/openusenet/openusenet/internal/archive"
	"github.com/openusenet/openusenet/internal/auth"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/feed"
	"github.com/openusenet/openusenet/internal/nntp"
	"github.com/openusenet/openusenet/internal/store"
)

type Server struct {
	cfg    config.Config
	st     store.Store
	mbox   *archive.MBox
	ln     net.Listener
	httpLn net.Listener
	log    *log.Logger
	feeder *feed.Feeder
}

func New(cfg config.Config, st store.Store, mbox *archive.MBox, lg *log.Logger) *Server {
	if lg == nil {
		lg = log.Default()
	}
	s := &Server{cfg: cfg, st: st, mbox: mbox, log: lg, feeder: feed.New(cfg, st, lg)}
	return s
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if err := SeedPeers(ctx, s.st, s.cfg); err != nil {
		return err
	}
	if err := BootstrapAdmin(ctx, s.st, s.log); err != nil {
		return err
	}
	go s.runArchiveSchedule(ctx)
	if err := s.serveHTTP(ctx); err != nil {
		return err
	}
	if err := s.serveNNTPTLS(ctx); err != nil {
		return err
	}
	addr := config.NormalizeListen(s.cfg.Listen.NNTP)
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	s.ln = ln
	s.log.Printf("nntp listening on %s (hostname %s)", ln.Addr(), s.cfg.Server.Hostname)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.log.Printf("accept: %v", err)
			continue
		}
		go func(c net.Conn) {
			nc := nntp.NewConn(c, s.cfg.Idle())
			nntp.Serve(nc, s.st, s.mbox, s.cfg, s.log, s.feeder)
		}(c)
	}
}

func (s *Server) serveHTTP(ctx context.Context) error {
	handler := admin.New(s.cfg, s.st, s.mbox, s.feeder, s.log).Handler()
	if err := s.listenHTTP(ctx, s.cfg.Listen.HTTP, false, handler); err != nil {
		return err
	}
	return s.listenHTTP(ctx, s.cfg.Listen.HTTPTLS, true, handler)
}

func (s *Server) listenHTTP(ctx context.Context, addr string, useTLS bool, handler http.Handler) error {
	addr = strings.TrimSpace(addr)
	if addr == "" || addr == "-" {
		return nil
	}
	if useTLS && !s.cfg.TLS.Enabled() {
		return fmt.Errorf("listen.http_tls set but tls.cert_file/key_file missing")
	}
	addr = config.NormalizeListen(addr)
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen http %s: %w", addr, err)
	}
	if useTLS {
		cert, err := tls.LoadX509KeyPair(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("tls load: %w", err)
		}
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	if s.httpLn == nil {
		s.httpLn = ln
	}
	hs := &http.Server{Handler: handler}
	kind := "http"
	if useTLS {
		kind = "https"
	}
	s.log.Printf("admin %s listening on %s", kind, ln.Addr())
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = hs.Shutdown(c)
	}()
	go func() {
		if err := hs.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Printf("http: %v", err)
		}
	}()
	return nil
}

func (s *Server) serveNNTPTLS(ctx context.Context) error {
	addr := strings.TrimSpace(s.cfg.Listen.NNTPTLS)
	if addr == "" || addr == "-" {
		return nil
	}
	if !s.cfg.TLS.Enabled() {
		return fmt.Errorf("listen.nntp_tls set but tls.cert_file/key_file missing")
	}
	addr = config.NormalizeListen(addr)
	cert, err := tls.LoadX509KeyPair(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
	if err != nil {
		return fmt.Errorf("tls load: %w", err)
	}
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen nntp tls %s: %w", addr, err)
	}
	tl := tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	s.log.Printf("nntp tls listening on %s", tl.Addr())
	go func() {
		<-ctx.Done()
		_ = tl.Close()
	}()
	go func() {
		for {
			c, err := tl.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				s.log.Printf("nntp tls accept: %v", err)
				continue
			}
			go func(c net.Conn) {
				nc := nntp.NewConn(c, s.cfg.Idle())
				nntp.Serve(nc, s.st, s.mbox, s.cfg, s.log, s.feeder)
			}(c)
		}
	}()
	return nil
}

func (s *Server) runArchiveSchedule(ctx context.Context) {
	sched := strings.ToLower(strings.TrimSpace(s.cfg.Archive.Schedule))
	var every time.Duration
	switch sched {
	case "daily":
		every = 24 * time.Hour
	case "weekly":
		every = 7 * 24 * time.Hour
	default:
		return
	}
	s.log.Printf("archive schedule %s (every %s)", sched, every)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			res, err := archive.Export(ctx, s.st, s.cfg.Archive.ExportDir, s.cfg.Archive.Groups)
			if err != nil {
				s.log.Printf("archive schedule: %v", err)
				continue
			}
			_ = archive.PruneOldExports(s.cfg.Archive.ExportDir, s.cfg.Archive.RetainGens)
			s.log.Printf("archive schedule wrote %d groups (%d articles) to %s", res.Groups, res.Articles, res.Dir)
		}
	}
}

func (s *Server) Addr() net.Addr {
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

// SeedPeers copies YAML peers into an empty peers table.
func SeedPeers(ctx context.Context, st store.Store, cfg config.Config) error {
	n, err := st.CountPeers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, p := range cfg.Peers {
		if _, err := st.CreatePeer(ctx, store.Peer{Host: p.Host, Port: p.Port, Enabled: true, Notes: "seeded from YAML"}); err != nil {
			return err
		}
	}
	return nil
}

// BootstrapAdmin creates the first admin from OPENUSENET_BOOTSTRAP_ADMIN=user:pass once.
func BootstrapAdmin(ctx context.Context, st store.Store, lg *log.Logger) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("OPENUSENET_BOOTSTRAP_ADMIN"))
	if raw == "" {
		return nil
	}
	user, pass, ok := strings.Cut(raw, ":")
	if !ok || user == "" || pass == "" {
		return fmt.Errorf("OPENUSENET_BOOTSTRAP_ADMIN must be user:pass")
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		return err
	}
	if _, err := st.CreateUser(ctx, store.User{
		Username: user, PasswordHash: hash, Role: store.RoleAdmin, CanPost: true,
	}); err != nil {
		return err
	}
	lg.Printf("bootstrap admin %q created from OPENUSENET_BOOTSTRAP_ADMIN", user)
	return nil
}

// Healthcheck dials NNTP and expects a 200/201 greeting plus CAPABILITIES.
func Healthcheck(addr string, timeout time.Duration) error {
	d := net.Dialer{Timeout: timeout}
	c, err := d.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	if err != nil {
		return err
	}
	line := string(buf[:n])
	if len(line) < 3 || (line[:3] != "200" && line[:3] != "201") {
		return fmt.Errorf("unexpected greeting: %q", line)
	}
	if _, err := c.Write([]byte("CAPABILITIES\r\nQUIT\r\n")); err != nil {
		return err
	}
	return nil
}
