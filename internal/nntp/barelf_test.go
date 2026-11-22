package nntp_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"openusenet/internal/nntp"
)

// A multi-line block ends only at CRLF "." CRLF. A "." on a line that a bare LF
// starts is body, not the terminator, so the bytes after it are not parsed as
// commands and no second article can be smuggled into the session.
func TestReadArticleBareLFNotTerminator(t *testing.T) {
	c, st := startTestServer(t)
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	_ = readLine(t, r)

	const outer = "<barelf-outer@test>"
	const smuggled = "<barelf-smuggled@test>"
	hdr := "Path: peer!not-for-mail\r\nFrom: a@b.c\r\nNewsgroups: local.test\r\n" +
		"Subject: s\r\nDate: Thu, 12 Nov 2026 00:00:00 +0000\r\nMessage-ID: " + outer + "\r\n\r\n"
	body := "hello\r\nx\n.\nTAKETHIS " + smuggled + "\r\n" +
		"Path: attacker!not-for-mail\r\nFrom: n@n\r\nNewsgroups: local.test\r\nSubject: x\r\n" +
		"Date: Thu, 12 Nov 2026 00:00:00 +0000\r\nMessage-ID: " + smuggled + "\r\n\r\nx\r\n"

	fmt.Fprintf(c, "IHAVE %s\r\n", outer)
	if l := readLine(t, r); !strings.HasPrefix(l, "335") {
		t.Fatalf("IHAVE: %q", l)
	}
	go fmt.Fprintf(c, "%s%s.\r\n", hdr, body)
	if l := readLine(t, r); !strings.HasPrefix(l, "235") {
		t.Fatalf("transfer: %q want 235", l)
	}
	// The session must be back in command state, not reading leftover body as
	// commands: a fresh STAT with no group selected gets 412, not 500.
	fmt.Fprintf(c, "STAT\r\n")
	if l := readLine(t, r); !strings.HasPrefix(l, "412") {
		t.Fatalf("after transfer: %q want 412 (session desynced)", l)
	}

	ctx := context.Background()
	if a, _ := st.GetByMsgID(ctx, outer); a == nil {
		t.Fatalf("outer %s not stored", outer)
	} else if !strings.Contains(a.Body, "TAKETHIS "+smuggled) {
		t.Errorf("smuggled lines were not kept as body: %q", a.Body)
	}
	if a, _ := st.GetByMsgID(ctx, smuggled); a != nil {
		t.Errorf("smuggled article %s was stored", smuggled)
	}
	if n, _ := st.CountArticles(ctx); n != 1 {
		t.Errorf("article count = %d, want 1", n)
	}
}

// ReadArticle at the wire level: only CRLF "." CRLF ends the block; a leading
// "." is unstuffed at a real line start; a bare LF or bare CR is kept as data
// (so a bare-LF "." does not terminate).
func TestReadArticleFramingUnit(t *testing.T) {
	a, b := net.Pipe()
	conn := nntp.NewConn(b, time.Second)
	wire := "..dot\r\n" + // -> .dot
		"...x\r\n" + // -> ..x
		"mid\rCR\r\n" + // bare CR kept
		"x\n.\nlast\r\n" + // bare LFs kept; the "." here is not a terminator
		".\r\n" // real terminator
	go func() { _, _ = io.WriteString(a, wire); _ = a.Close() }()
	got, err := conn.ReadArticle(0)
	if err != nil {
		t.Fatal(err)
	}
	want := ".dot\r\n..x\r\nmid\rCR\r\nx\n.\nlast\r\n"
	if string(got) != want {
		t.Errorf("ReadArticle = %q, want %q", got, want)
	}
}
