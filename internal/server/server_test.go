package server_test

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/server"
	"github.com/openusenet/openusenet/internal/store"
)

func waitAddr(t *testing.T, srv *server.Server) net.Addr {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a := srv.Addr(); a != nil {
			return a
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("listen timeout")
	return nil
}

func TestPOSTFeedsIHAVE(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stA := store.NewMemory()
	stB := store.NewMemory()
	if err := stA.EnsureGroup(ctx, "local.test", "Local test group", "y"); err != nil {
		t.Fatal(err)
	}
	if err := stB.EnsureGroup(ctx, "local.test", "Local test group", "y"); err != nil {
		t.Fatal(err)
	}

	cfgB := config.Defaults()
	cfgB.Server.Hostname = "news-b"
	cfgB.Server.Pathhost = "news-b"
	cfgB.Listen.NNTP = "127.0.0.1:0"
	srvB := server.New(cfgB, stB, nil, nil)
	go func() { _ = srvB.ListenAndServe(ctx) }()
	addrB := waitAddr(t, srvB)
	_, portStr, err := net.SplitHostPort(addrB.String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	cfgA := config.Defaults()
	cfgA.Server.Hostname = "news-a"
	cfgA.Server.Pathhost = "news-a"
	cfgA.Listen.NNTP = "127.0.0.1:0"
	cfgA.Peers = []config.Peer{{Host: "127.0.0.1", Port: port}}
	srvA := server.New(cfgA, stA, nil, nil)
	go func() { _ = srvA.ListenAndServe(ctx) }()
	addrA := waitAddr(t, srvA)

	c, err := net.Dial("tcp", addrA.String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	greet, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(greet, "200 ") {
		t.Fatalf("greeting %q %v", greet, err)
	}
	if _, err := c.Write([]byte("POST\r\n")); err != nil {
		t.Fatal(err)
	}
	cont, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(cont, "340 ") {
		t.Fatalf("post cont %q %v", cont, err)
	}
	art := "From: tester@example.com\r\n" +
		"Newsgroups: local.test\r\n" +
		"Subject: mesh\r\n" +
		"\r\n" +
		"hello peers\r\n" +
		".\r\n"
	if _, err := c.Write([]byte(art)); err != nil {
		t.Fatal(err)
	}
	posted, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(posted, "240 ") {
		t.Fatalf("posted %q %v", posted, err)
	}
	fields := strings.Fields(posted)
	var msgid string
	for _, f := range fields {
		if strings.HasPrefix(f, "<") {
			msgid = f
			break
		}
	}
	if msgid == "" {
		t.Fatalf("no msgid in %q", posted)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ok, err := stB.HasMessageID(ctx, msgid)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			got, err := stB.GetByMsgID(ctx, msgid)
			if err != nil || got == nil {
				t.Fatalf("get %v %v", got, err)
			}
			if !strings.Contains(got.Headers, "Path: news-b!news-a!") {
				t.Fatalf("expected path hop, got %q", got.Headers)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("news-b did not receive %s", msgid)
}
