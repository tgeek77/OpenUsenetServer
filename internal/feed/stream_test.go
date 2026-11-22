package feed

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"openusenet/internal/config"
	"openusenet/internal/store"
)

func TestModeStreamFallback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var sawModeStream, sawCheck, sawTakeThis atomic.Bool
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = c.Write([]byte("200 diablo-like ready\r\n"))
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimRight(line, "\r\n")
					fields := strings.Fields(line)
					if len(fields) == 0 {
						continue
					}
					switch strings.ToUpper(fields[0]) {
					case "CAPABILITIES":
						_, _ = c.Write([]byte("500 Syntax error or bad command\r\n"))
					case "MODE":
						sawModeStream.Store(true)
						_, _ = c.Write([]byte("203 Streaming permitted\r\n"))
					case "CHECK":
						sawCheck.Store(true)
						_, _ = c.Write([]byte("238 " + fields[1] + " send it\r\n"))
					case "TAKETHIS":
						sawTakeThis.Store(true)
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if strings.TrimRight(l, "\r\n") == "." {
								break
							}
						}
						_, _ = c.Write([]byte("239 " + fields[1] + " article transferred\r\n"))
					case "QUIT":
						_, _ = c.Write([]byte("205 bye\r\n"))
						return
					}
				}
			}(c)
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	cfg := config.Defaults()
	cfg.Server.Hostname, cfg.Server.Pathhost = "news-a", "news-a"
	f := New(cfg, nil, nil, nil)
	defer f.Close()

	wire := []byte("Path: news-a!not-for-mail\r\nFrom: a@b\r\nNewsgroups: local.test\r\nSubject: s\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMessage-ID: <m1@test>\r\n\r\nbody\r\n")
	accepted, err := f.deliverArticle(store.Peer{ID: 1, Host: host, Port: port, Enabled: true}, "<m1@test>", wire)
	if err != nil || !accepted {
		t.Fatalf("deliverArticle: accepted=%v err=%v", accepted, err)
	}
	if !sawModeStream.Load() || !sawCheck.Load() || !sawTakeThis.Load() {
		t.Fatalf("expected MODE STREAM + CHECK + TAKETHIS, got mode=%v check=%v takethis=%v",
			sawModeStream.Load(), sawCheck.Load(), sawTakeThis.Load())
	}
}

func TestPersistentPipelinedFeed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var connCount, maxConcurrent, activeConns, transferred atomic.Int64
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			connCount.Add(1)
			cur := activeConns.Add(1)
			for {
				prev := maxConcurrent.Load()
				if cur <= prev || maxConcurrent.CompareAndSwap(prev, cur) {
					break
				}
			}
			go func(c net.Conn) {
				defer func() {
					activeConns.Add(-1)
					_ = c.Close()
				}()
				_, _ = c.Write([]byte("200 ready\r\n"))
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimRight(line, "\r\n")
					fields := strings.Fields(line)
					if len(fields) == 0 {
						continue
					}
					switch strings.ToUpper(fields[0]) {
					case "CAPABILITIES":
						_, _ = c.Write([]byte("101 caps\r\nVERSION 2\r\nSTREAMING\r\n.\r\n"))
					case "CHECK":
						_, _ = c.Write([]byte("238 " + fields[1] + " want\r\n"))
					case "TAKETHIS":
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if strings.TrimRight(l, "\r\n") == "." {
								break
							}
						}
						transferred.Add(1)
						_, _ = c.Write([]byte("239 " + fields[1] + " transferred\r\n"))
					case "QUIT":
						_, _ = c.Write([]byte("205 bye\r\n"))
						return
					}
				}
			}(c)
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	ctx := context.Background()
	st := store.NewMemory()
	_ = st.EnsureGroup(ctx, "local.test", "", "y")
	if _, err := st.CreatePeer(ctx, store.Peer{
		Name: "fake-peer", Host: host, Port: port, Enabled: true, Patterns: "*",
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Server.Hostname, cfg.Server.Pathhost = "news-a", "news-a"
	f := New(cfg, st, st, nil)
	defer f.Close()

	const nArts = 10
	for i := 0; i < nArts; i++ {
		id := fmt.Sprintf("<pipe-%d@news-a>", i)
		hdr := fmt.Sprintf("Path: news-a!not-for-mail\r\nFrom: a@b\r\nNewsgroups: local.test\r\nSubject: s%d\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMessage-ID: %s", i, id)
		body := "hello\r\n"
		if _, err := st.Post(ctx, hdr, body, id, "s", "a@b", "Mon, 01 Jan 2024 00:00:00 +0000", "", "news-a", len(hdr)+4+len(body), 1, []string{"local.test"}, false); err != nil {
			t.Fatal(err)
		}
		f.Offer(id, "news-a!not-for-mail", []string{"local.test"}, []byte(hdr+"\r\n\r\n"+body))
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if transferred.Load() == nArts {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if transferred.Load() != nArts {
		t.Fatalf("transferred %d/%d", transferred.Load(), nArts)
	}
	if connCount.Load() != 1 {
		t.Fatalf("expected 1 persistent connection for %d articles, got %d", nArts, connCount.Load())
	}
	if maxConcurrent.Load() != 1 {
		t.Fatalf("expected max 1 concurrent connection, got %d", maxConcurrent.Load())
	}
}
