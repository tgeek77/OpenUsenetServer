package nntp_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/openusenet/openusenet/internal/article"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/nntp"
	"github.com/openusenet/openusenet/internal/store"
)

func startTestServer(t *testing.T) (net.Conn, *store.Memory) {
	t.Helper()
	a, b := net.Pipe()
	st := store.NewMemory()
	if err := st.EnsureGroup(context.Background(), "local.test", "Local test group", "y"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Server.Hostname = "news.test"
	cfg.Server.Pathhost = "news.test"
	cfg.Limits.IdleSeconds = 30
	go nntp.Serve(nntp.NewConn(b, 30*time.Second), st, nil, cfg, nil, nil)
	t.Cleanup(func() { _ = a.Close() })
	return a, st
}

func readLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	s, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(s, "\r\n")
}

func readBlock(t *testing.T, r *bufio.Reader) (string, []string) {
	t.Helper()
	first := readLine(t, r)
	var lines []string
	for {
		l := readLine(t, r)
		if l == "." {
			break
		}
		if strings.HasPrefix(l, ".") {
			l = l[1:]
		}
		lines = append(lines, l)
	}
	return first, lines
}

func TestGreetingAndCapabilities(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	greet := readLine(t, r)
	if !strings.HasPrefix(greet, "200 ") {
		t.Fatalf("greeting %q", greet)
	}
	_, _ = c.Write([]byte("CAPABILITIES\r\n"))
	first, lines := readBlock(t, r)
	if !strings.HasPrefix(first, "101 ") {
		t.Fatalf("cap %q", first)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"VERSION 2", "READER", "POST", "LIST ", "OVER MSGID"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("capabilities missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "STREAMING") {
		t.Fatalf("should not advertise STREAMING yet: %q", joined)
	}
	if !strings.Contains(joined, "IHAVE") {
		t.Fatalf("should advertise IHAVE: %q", joined)
	}
	_, _ = c.Write([]byte("QUIT\r\n"))
	if q := readLine(t, r); !strings.HasPrefix(q, "205 ") {
		t.Fatalf("quit %q", q)
	}
}

func TestUnknownAndSyntax(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	_, _ = c.Write([]byte("MAIL\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "500 ") {
		t.Fatalf("unknown %q", l)
	}
	_, _ = c.Write([]byte("MODE POSTER\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "501 ") {
		t.Fatalf("mode %q", l)
	}
}

func TestGroupListPostArticle(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)

	_, _ = c.Write([]byte("LIST ACTIVE\r\n"))
	first, lines := readBlock(t, r)
	if !strings.HasPrefix(first, "215 ") {
		t.Fatalf("list %q", first)
	}
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "local.test ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing local.test in %v", lines)
	}

	_, _ = c.Write([]byte("GROUP missing.group\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "411 ") {
		t.Fatalf("missing group %q", l)
	}

	_, _ = c.Write([]byte("POST\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "340 ") {
		t.Fatalf("post cont %q", l)
	}
	art := "From: tester@example.com\r\n" +
		"Newsgroups: local.test\r\n" +
		"Subject: hello\r\n" +
		"\r\n" +
		"body line\r\n" +
		".\r\n"
	_, _ = c.Write([]byte(art))
	posted := readLine(t, r)
	if !strings.HasPrefix(posted, "240 ") {
		t.Fatalf("posted %q", posted)
	}

	_, _ = c.Write([]byte("GROUP local.test\r\n"))
	gline := readLine(t, r)
	if !strings.HasPrefix(gline, "211 ") || !strings.Contains(gline, "local.test") {
		t.Fatalf("group %q", gline)
	}

	_, _ = c.Write([]byte("ARTICLE 1\r\n"))
	afirst, alines := readBlock(t, r)
	if !strings.HasPrefix(afirst, "220 ") {
		t.Fatalf("article %q", afirst)
	}
	blob := strings.Join(alines, "\n")
	if !strings.Contains(blob, "Subject: hello") || !strings.Contains(blob, "body line") {
		t.Fatalf("article body %q", blob)
	}
	if !strings.Contains(blob, "Message-ID:") {
		t.Fatalf("missing injected Message-ID")
	}

	_, _ = c.Write([]byte("STAT 1\r\nARTICLE <no-such@id>\r\n"))
	stat := readLine(t, r)
	if !strings.HasPrefix(stat, "223 ") {
		t.Fatalf("stat %q", stat)
	}
	nf := readLine(t, r)
	if !strings.HasPrefix(nf, "430 ") {
		t.Fatalf("msgid miss %q", nf)
	}

	_, _ = c.Write([]byte("OVER 1-1\r\n"))
	ofirst, olines := readBlock(t, r)
	if !strings.HasPrefix(ofirst, "224 ") {
		t.Fatalf("over %q", ofirst)
	}
	if len(olines) != 1 || !strings.Contains(olines[0], "hello") {
		t.Fatalf("overview %v", olines)
	}
}

func TestFetchByMsgidDoesNotChangeCurrent(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	_, _ = c.Write([]byte("POST\r\n"))
	_ = readLine(t, r)
	_, _ = c.Write([]byte("From: a@b.c\r\nNewsgroups: local.test\r\nSubject: one\r\n\r\nx\r\n.\r\n"))
	_ = readLine(t, r)
	_, _ = c.Write([]byte("POST\r\n"))
	_ = readLine(t, r)
	_, _ = c.Write([]byte("From: a@b.c\r\nNewsgroups: local.test\r\nSubject: two\r\n\r\ny\r\n.\r\n"))
	_ = readLine(t, r)

	_, _ = c.Write([]byte("GROUP local.test\r\n"))
	_ = readLine(t, r)
	_, _ = c.Write([]byte("STAT 1\r\n"))
	s1 := readLine(t, r)
	parts := strings.Fields(s1)
	if len(parts) < 3 {
		t.Fatalf("stat %q", s1)
	}
	msgid1 := parts[2]
	_, _ = c.Write([]byte("STAT 2\r\n"))
	s2 := readLine(t, r)
	msgid2 := strings.Fields(s2)[2]
	_, _ = c.Write([]byte("ARTICLE " + msgid1 + "\r\n"))
	first, _ := readBlock(t, r)
	if !strings.HasPrefix(first, "220 0 ") {
		t.Fatalf("msgid fetch should report number 0: %q", first)
	}
	_, _ = c.Write([]byte("STAT\r\n"))
	cur := readLine(t, r)
	if !strings.Contains(cur, msgid2) {
		t.Fatalf("current should still be article 2 (%s), got %q", msgid2, cur)
	}
}

func TestPipelinedGroupStat(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	_, _ = c.Write([]byte("POST\r\n"))
	_ = readLine(t, r)
	_, _ = c.Write([]byte("From: a@b.c\r\nNewsgroups: local.test\r\nSubject: p\r\n\r\nz\r\n.\r\n"))
	_ = readLine(t, r)
	_, _ = c.Write([]byte("GROUP local.test\r\nSTAT\r\n"))
	g := readLine(t, r)
	s := readLine(t, r)
	if !strings.HasPrefix(g, "211 ") {
		t.Fatalf("group %q", g)
	}
	if !strings.HasPrefix(s, "223 ") {
		t.Fatalf("stat %q", s)
	}
}

func TestInjectRequiresFrom(t *testing.T) {
	a, err := article.Parse([]byte("Newsgroups: local.test\r\nSubject: x\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := article.InjectForPost(a, article.InjectOpts{Hostname: "h", Pathhost: "h"}); err == nil {
		t.Fatal("expected missing From")
	}
}

func TestIHaveAcceptAndDuplicate(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	msgid := "<ihave-1@news.test>"
	_, _ = c.Write([]byte("IHAVE " + msgid + "\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "335 ") {
		t.Fatalf("ihave cont %q", l)
	}
	art := "Path: other!not-for-mail\r\n" +
		"From: a@b.c\r\n" +
		"Newsgroups: local.test\r\n" +
		"Subject: ihave\r\n" +
		"Date: Mon, 01 Jan 2024 00:00:00 +0000\r\n" +
		"Message-ID: " + msgid + "\r\n" +
		"\r\n" +
		"body\r\n" +
		".\r\n"
	_, _ = c.Write([]byte(art))
	if l := readLine(t, r); !strings.HasPrefix(l, "235 ") {
		t.Fatalf("ihave ok %q", l)
	}
	_, _ = c.Write([]byte("ARTICLE " + msgid + "\r\n"))
	first, lines := readBlock(t, r)
	if !strings.HasPrefix(first, "220 ") {
		t.Fatalf("article %q", first)
	}
	blob := strings.Join(lines, "\n")
	if !strings.Contains(blob, "Path: news.test!other!not-for-mail") {
		t.Fatalf("path not prepended: %q", blob)
	}
	_, _ = c.Write([]byte("IHAVE " + msgid + "\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "435 ") {
		t.Fatalf("ihave dup %q", l)
	}
}

func TestIHavePathLoop(t *testing.T) {
	c, _ := startTestServer(t)
	r := bufio.NewReader(c)
	_ = readLine(t, r)
	msgid := "<loop-1@news.test>"
	_, _ = c.Write([]byte("IHAVE " + msgid + "\r\n"))
	if l := readLine(t, r); !strings.HasPrefix(l, "335 ") {
		t.Fatalf("ihave cont %q", l)
	}
	art := "Path: news.test!other!not-for-mail\r\n" +
		"From: a@b.c\r\n" +
		"Newsgroups: local.test\r\n" +
		"Subject: loop\r\n" +
		"Date: Mon, 01 Jan 2024 00:00:00 +0000\r\n" +
		"Message-ID: " + msgid + "\r\n" +
		"\r\n" +
		"x\r\n" +
		".\r\n"
	_, _ = c.Write([]byte(art))
	if l := readLine(t, r); !strings.HasPrefix(l, "437 ") {
		t.Fatalf("want path-loop reject, got %q", l)
	}
}

