package nntp_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"openusenet/internal/auth"
	"openusenet/internal/config"
	"openusenet/internal/nntp"
	"openusenet/internal/store"
)

func TestAuthinfoRFC4643(t *testing.T) {
	c, st := startTestServer(t)
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(context.Background(), store.User{Username: "alice", PasswordHash: hash, CanPost: true}); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	cmd := func(line string) string {
		t.Helper()
		if _, err := c.Write([]byte(line + "\r\n")); err != nil {
			t.Fatal(err)
		}
		return readLine(t, r)
	}
	caps := func() string {
		t.Helper()
		_, _ = c.Write([]byte("CAPABILITIES\r\n"))
		_, lines := readBlock(t, r)
		return strings.Join(lines, "\n")
	}

	if !strings.Contains(caps(), "AUTHINFO USER") {
		t.Fatal("AUTHINFO USER not advertised before login")
	}
	if l := cmd("AUTHINFO USER alice extra"); !strings.HasPrefix(l, "501 ") {
		t.Fatalf("user with an extra argument %q", l)
	}
	if l := cmd("AUTHINFO PASS secret"); !strings.HasPrefix(l, "482 ") {
		t.Fatalf("pass without user %q", l)
	}
	if l := cmd("AUTHINFO USER alice"); !strings.HasPrefix(l, "381 ") {
		t.Fatalf("user %q", l)
	}
	if l := cmd("AUTHINFO PASS wrong"); !strings.HasPrefix(l, "481 ") {
		t.Fatalf("wrong pass %q", l)
	}
	if l := cmd("AUTHINFO USER alice"); !strings.HasPrefix(l, "381 ") {
		t.Fatalf("retry user %q", l)
	}
	if l := cmd("AUTHINFO PASS secret"); !strings.HasPrefix(l, "281 ") {
		t.Fatalf("retry pass %q", l)
	}
	if l := cmd("AUTHINFO USER alice"); !strings.HasPrefix(l, "502 ") {
		t.Fatalf("user after auth %q", l)
	}
	if l := cmd("AUTHINFO PASS secret"); !strings.HasPrefix(l, "502 ") {
		t.Fatalf("pass after auth %q", l)
	}
	// The SASL form has four words; it must reach the "already authenticated" check too.
	if l := cmd("AUTHINFO SASL PLAIN ="); !strings.HasPrefix(l, "502 ") {
		t.Fatalf("sasl after auth %q", l)
	}
	if strings.Contains(caps(), "AUTHINFO") {
		t.Fatal("AUTHINFO still advertised after login")
	}
	if l := cmd("POST"); !strings.HasPrefix(l, "340 ") {
		t.Fatalf("session no longer authenticated, post %q", l)
	}
	if l := cmd("From: alice@example.com\r\nNewsgroups: local.test\r\nSubject: auth\r\n\r\nhi\r\n."); !strings.HasPrefix(l, "240 ") {
		t.Fatalf("post after auth %q", l)
	}
}

// After a peer logs in with its feed password (feedAuthOK), every further AUTHINFO, the SASL
// form included, gets 502 (RFC 4643 2.2), CAPABILITIES no longer lists AUTHINFO, and the
// session keeps transferring articles. Written by ous-sol in the review of this change.
func TestAuthinfoAfterFeedLogin(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if err := st.EnsureGroup(ctx, "local.test", "review fixture", "y"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreatePeer(ctx, store.Peer{Name: "review-feed", Host: "pipe", IncomingHost: "pipe", Enabled: true, IncomingPassword: "feed-fixture-secret", Patterns: "local.*"}); err != nil {
		t.Fatal(err)
	}
	c, serverConn := net.Pipe()
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	cfg := config.Defaults()
	cfg.Server.Hostname, cfg.Server.Pathhost = "news.test", "news.test"
	cfg.Limits.ArtCutoffDays = 0
	go nntp.Serve(nntp.NewConn(serverConn, 5*time.Second), st, nil, cfg, nil, nil, nil, nil)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	expect := func(command, code string) {
		t.Helper()
		if _, err := c.Write([]byte(command + "\r\n")); err != nil {
			t.Fatal(err)
		}
		if line := readLine(t, r); !strings.HasPrefix(line, code+" ") {
			t.Fatalf("%s: got %q, want %s", strings.Fields(command)[0], line, code)
		}
	}
	expect("AUTHINFO USER review-feed", "381")
	expect("AUTHINFO PASS feed-fixture-secret", "281")
	expect("AUTHINFO USER review-feed", "502")
	expect("AUTHINFO PASS feed-fixture-secret", "502")
	expect("AUTHINFO SASL PLAIN =", "502")
	if _, err := c.Write([]byte("CAPABILITIES\r\n")); err != nil {
		t.Fatal(err)
	}
	status, caps := readBlock(t, r)
	if !strings.HasPrefix(status, "101 ") || strings.Contains(strings.Join(caps, "\n"), "AUTHINFO") {
		t.Fatalf("capabilities after feed login: %q %q", status, caps)
	}
	const id = "<auth-review-feed@example.invalid>"
	expect("CHECK "+id, "238")
	expect("IHAVE "+id, "335")
	expect("Path: remote!not-for-mail\r\nFrom: feed@example.invalid\r\nNewsgroups: local.test\r\nSubject: review continuity\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMessage-ID: "+id+"\r\n\r\nbody\r\n.", "235")
	expect("CHECK "+id, "438")
}
