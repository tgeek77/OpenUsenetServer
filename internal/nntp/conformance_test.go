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

func TestRefusedPostSingleReply(t *testing.T) {
	c, st := startTestServer(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := st.CreateUser(context.Background(), store.User{Username: "bob", PasswordHash: hash, CanPost: false}); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	cmd := func(line string) string {
		t.Helper()
		_, _ = c.Write([]byte(line + "\r\n"))
		return readLine(t, r)
	}
	// Unauthenticated POST must send 480 and NOT 340; next command must work immediately.
	if l := cmd("POST"); !strings.HasPrefix(l, "480 ") {
		t.Fatalf("unauth POST got %q, want 480", l)
	}
	if l := cmd("DATE"); !strings.HasPrefix(l, "111 ") {
		t.Fatalf("after 480 POST, DATE got %q (session out of sync)", l)
	}
	// Login as user without CanPost: POST must send 440 and NOT 340.
	if l := cmd("AUTHINFO USER bob"); !strings.HasPrefix(l, "381 ") {
		t.Fatalf("user got %q", l)
	}
	if l := cmd("AUTHINFO PASS secret"); !strings.HasPrefix(l, "281 ") {
		t.Fatalf("pass got %q", l)
	}
	if l := cmd("POST"); !strings.HasPrefix(l, "440 ") {
		t.Fatalf("no-post user POST got %q, want 440", l)
	}
	if l := cmd("DATE"); !strings.HasPrefix(l, "111 ") {
		t.Fatalf("after 440 POST, DATE got %q (session out of sync)", l)
	}
}

func TestInjectionInfoPostingAccount(t *testing.T) {
	c, st := startTestServer(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := st.CreateUser(context.Background(), store.User{Username: "alice", PasswordHash: hash, CanPost: true}); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	cmd := func(line string) string {
		t.Helper()
		_, _ = c.Write([]byte(line + "\r\n"))
		return readLine(t, r)
	}
	if l := cmd("AUTHINFO USER alice"); !strings.HasPrefix(l, "381 ") {
		t.Fatalf("user got %q", l)
	}
	if l := cmd("AUTHINFO PASS secret"); !strings.HasPrefix(l, "281 ") {
		t.Fatalf("pass got %q", l)
	}
	if l := cmd("POST"); !strings.HasPrefix(l, "340 ") {
		t.Fatalf("post got %q", l)
	}
	if l := cmd("From: alice@example.com\r\nNewsgroups: local.test\r\nSubject: auth\r\n\r\nhi\r\n."); !strings.HasPrefix(l, "240 ") {
		t.Fatalf("post body got %q", l)
	}
	_, _ = c.Write([]byte("GROUP local.test\r\n"))
	_ = readLine(t, r)
	_, _ = c.Write([]byte("HEAD 1\r\n"))
	_, hdrLines := readBlock(t, r)
	joined := strings.Join(hdrLines, "\n")
	if !strings.Contains(joined, `posting-account="alice"`) {
		t.Fatalf("expected posting-account=\"alice\" in Injection-Info, got:\n%s", joined)
	}
}

func TestTakeThisReplyTextNoDuplicateID(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	const id = "<takethis-o06@example.test>"
	_, _ = c.Write([]byte("TAKETHIS " + id + "\r\n" +
		"Path: remote!not-for-mail\r\n" +
		"From: a@b.test\r\n" +
		"Newsgroups: local.test\r\n" +
		"Subject: o06\r\n" +
		"Date: Mon, 01 Jan 2024 00:00:00 +0000\r\n" +
		"Message-ID: " + id + "\r\n\r\nbody\r\n.\r\n"))
	line := readLine(t, r)
	if line != "239 "+id+" article transferred" {
		t.Fatalf("got %q, want %q", line, "239 "+id+" article transferred")
	}
}

func TestOverAndHdrNoCurrentArticle(t *testing.T) {
	c, st := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	cmd := func(line string) string {
		t.Helper()
		_, _ = c.Write([]byte(line + "\r\n"))
		return readLine(t, r)
	}
	if l := cmd("GROUP local.test"); !strings.HasPrefix(l, "211 ") {
		t.Fatalf("group got %q", l)
	}
	if l := cmd("OVER"); !strings.HasPrefix(l, "420 ") {
		t.Fatalf("OVER in empty group got %q, want 420", l)
	}
	if l := cmd("HDR Subject"); !strings.HasPrefix(l, "420 ") {
		t.Fatalf("HDR in empty group got %q, want 420", l)
	}
	if l := cmd("POST"); !strings.HasPrefix(l, "340 ") {
		t.Fatalf("post got %q", l)
	}
	const id = "<cancel-me-o23@example.test>"
	if l := cmd("From: a@b.test\r\nNewsgroups: local.test\r\nSubject: temp\r\nMessage-ID: " + id + "\r\n\r\nhi\r\n."); !strings.HasPrefix(l, "240 ") {
		t.Fatalf("post body got %q", l)
	}
	if l := cmd("GROUP local.test"); !strings.HasPrefix(l, "211 ") {
		t.Fatalf("group got %q", l)
	}
	if _, err := st.CancelMessageID(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if l := cmd("OVER"); !strings.HasPrefix(l, "420 ") {
		t.Fatalf("OVER on cancelled current article got %q, want 420", l)
	}
	if l := cmd("HDR Subject"); !strings.HasPrefix(l, "420 ") {
		t.Fatalf("HDR on cancelled current article got %q, want 420", l)
	}
}

func TestOverMsgIDBytesAndServedXref(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	const id = "<xref-bytes-o10-o11@example.test>"
	_, _ = c.Write([]byte("POST\r\n"))
	_ = readLine(t, r)
	_, _ = c.Write([]byte("From: a@b.test\r\nNewsgroups: local.test\r\nSubject: bytes test\r\nMessage-ID: " + id + "\r\nX-Extra: keep-me\r\n\r\nhello world\r\n.\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "240 ") {
		t.Fatalf("post got %q", l)
	}
	_, _ = c.Write([]byte("GROUP local.test\r\n"))
	_ = readLine(t, r)

	_, _ = c.Write([]byte("HEAD 1\r\n"))
	_, headLines := readBlock(t, r)
	hasXref := false
	for _, l := range headLines {
		if strings.HasPrefix(l, "Xref: news.test local.test:1") {
			hasXref = true
		}
	}
	if !hasXref {
		t.Fatalf("HEAD missing Xref header: %q", headLines)
	}

	_, _ = c.Write([]byte("OVER 1\r\n"))
	_, overNum := readBlock(t, r)
	_, _ = c.Write([]byte("OVER " + id + "\r\n"))
	_, overID := readBlock(t, r)
	if len(overNum) != 1 || len(overID) != 1 {
		t.Fatalf("unexpected over lines: %q vs %q", overNum, overID)
	}
	fNum := strings.Split(overNum[0], "\t")
	fID := strings.Split(overID[0], "\t")
	if len(fNum) < 8 || len(fID) < 8 {
		t.Fatalf("short over rows: %q vs %q", overNum[0], overID[0])
	}
	if fNum[6] != fID[6] {
		t.Fatalf("OVER :bytes mismatch: by-num=%s by-msgid=%s", fNum[6], fID[6])
	}
}

func TestRawBytesHeaderPreservationAndRejectMalformed(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)

	// O-16: header line with spaces before colon (mbox "From " line) must be rejected.
	_, _ = c.Write([]byte("IHAVE <bad-hdr@example.test>\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "335 ") {
		t.Fatalf("ihave got %q", l)
	}
	_, _ = c.Write([]byte("From nobody Thu Dec  8 12:55:04 2016\r\n" +
		"From: a@b.test\r\nNewsgroups: local.test\r\nSubject: bad\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMessage-ID: <bad-hdr@example.test>\r\n\r\nbody\r\n.\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "437 ") {
		t.Fatalf("malformed header got %q, want 437", l)
	}

	// O-05, O-13, O-15: preserve header order, folding, bare CR, 8-bit Latin-1, NUL byte.
	const id = "<preserve-bytes@example.test>"
	_, _ = c.Write([]byte("IHAVE " + id + "\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "335 ") {
		t.Fatalf("ihave got %q", l)
	}
	rawBody := "col1\rcol2\r\ncaf\xe9\x00end\r\n"
	art := "X-Zebra: first\r\n" +
		"Subject: folded\r\n" +
		"\tcontinuation\r\n" +
		"Message-Id: " + id + "\r\n" +
		"Date: Mon, 01 Jan 2024 00:00:00 +0000\r\n" +
		"From: a@b.test\r\n" +
		"Newsgroups: local.test\r\n" +
		"Path: remote!not-for-mail\r\n" +
		"X-Alpha: last\r\n" +
		"\r\n" +
		rawBody +
		".\r\n"
	_, _ = c.Write([]byte(art))
	if l := readLine(t, r); !strings.HasPrefix(l, "235 ") {
		t.Fatalf("ihave preserve got %q", l)
	}

	_, _ = c.Write([]byte("ARTICLE " + id + "\r\n"))
	status := readLine(t, r)
	if !strings.HasPrefix(status, "220 ") {
		t.Fatalf("article got %q", status)
	}
	var gotLines []string
	for {
		var buf []byte
		for {
			b, err := r.ReadByte()
			if err != nil {
				t.Fatal(err)
			}
			buf = append(buf, b)
			if len(buf) >= 2 && buf[len(buf)-2] == '\r' && buf[len(buf)-1] == '\n' {
				break
			}
		}
		line := string(buf[:len(buf)-2])
		if line == "." {
			break
		}
		gotLines = append(gotLines, line)
	}
	joined := strings.Join(gotLines, "\r\n")
	if !strings.Contains(joined, "X-Zebra: first\r\nSubject: folded\r\n\tcontinuation\r\nMessage-Id: "+id) {
		t.Fatalf("header order/folding/casing not preserved:\n%q", joined)
	}
	if !strings.Contains(joined, "col1\rcol2\r\ncaf\xe9\x00end") {
		t.Fatalf("bare CR / 8-bit / NUL body not preserved:\n%q", joined)
	}
}

func TestHostnamePeerPatternsApply(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	_ = st.EnsureGroup(ctx, "local.test", "", "y")
	_ = st.EnsureGroup(ctx, "alt.test", "", "y")
	if _, err := st.CreatePeer(ctx, store.Peer{
		Name: "localhost-peer", Host: "localhost", IncomingHost: "localhost",
		Enabled: true, Patterns: "local.*",
	}); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	closedInbound := false
	cfg := config.Defaults()
	cfg.Server.Hostname, cfg.Server.Pathhost = "news.test", "news.test"
	cfg.Inbound.Open = &closedInbound
	cfg.Limits.ArtCutoffDays = 0

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		nntp.Serve(nntp.NewConn(conn, 5*time.Second), st, nil, cfg, nil, nil, nil, nil)
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	_ = readLine(t, r)

	_, _ = c.Write([]byte("IHAVE <o24-denied@example.test>\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "335 ") {
		t.Fatalf("ihave got %q", l)
	}
	_, _ = c.Write([]byte("Path: remote!not-for-mail\r\nFrom: a@b.test\r\nNewsgroups: alt.test\r\nSubject: denied\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMessage-ID: <o24-denied@example.test>\r\n\r\nbody\r\n.\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "437 ") {
		t.Fatalf("hostname peer feeding disallowed group got %q, want 437", l)
	}
}
