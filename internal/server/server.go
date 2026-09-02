package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/openusenet/openusenet/internal/admin"
	"github.com/openusenet/openusenet/internal/archive"
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
	return &Server{cfg: cfg, st: st, mbox: mbox, log: lg, feeder: feed.New(cfg, lg)}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if err := s.serveHTTP(ctx); err != nil {
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
	httpAddr := strings.TrimSpace(s.cfg.Listen.HTTP)
	if httpAddr == "" || httpAddr == "-" {
		return nil
	}
	httpAddr = config.NormalizeListen(httpAddr)
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", httpAddr)
	if err != nil {
		return fmt.Errorf("listen http %s: %w", httpAddr, err)
	}
	s.httpLn = ln
	hs := &http.Server{Handler: admin.New(s.cfg, s.st, s.feeder).Handler()}
	s.log.Printf("admin http listening on %s (no authentication)", ln.Addr())
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

func (s *Server) Addr() net.Addr {
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
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
