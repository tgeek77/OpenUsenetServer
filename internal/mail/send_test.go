package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSendPlainNoSTARTTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go serveSMTP(t, ln, false, got)

	err = Send(context.Background(), Settings{
		Host: "127.0.0.1", Port: portOf(ln), Security: SecurityPlain,
		Username: "news", Password: "secret", From: "news@example.org",
	}, Message{To: []string{"top1000@example.net"}, Subject: "hello", Body: "report\n"})
	if err != nil {
		t.Fatal(err)
	}
	body := <-got
	if strings.Contains(body, "STARTTLS") {
		t.Fatalf("plain mode sent STARTTLS:\n%s", body)
	}
	if !strings.Contains(body, "AUTH PLAIN") || !strings.Contains(body, "Subject: hello") || !strings.Contains(body, "report") {
		t.Fatalf("transcript:\n%s", body)
	}
}

func TestSendRawKeepsArticle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go serveSMTP(t, ln, false, got)
	raw := []byte("To: misc-test-moderated@moderators.isc.org\r\nFrom: poster@example.org\r\nSubject: hello\r\n\r\nbody\r\n")
	err = SendRaw(context.Background(), Settings{
		Host: "127.0.0.1", Port: portOf(ln), Security: SecurityPlain, From: "news@news.example.org",
	}, []string{"misc-test-moderated@moderators.isc.org"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	body := <-got
	if !strings.Contains(body, "MAIL FROM:<news@news.example.org>") || !strings.Contains(body, "RCPT TO:<misc-test-moderated@moderators.isc.org>") {
		t.Fatalf("envelope:\n%s", body)
	}
	if !strings.Contains(body, "From: poster@example.org") || strings.Count(body, "Subject:") != 1 || strings.Contains(body, "MIME-Version") {
		t.Fatalf("data was wrapped:\n%s", body)
	}
}

func TestSendImplicitTLS(t *testing.T) {
	cert, err := testCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go serveSMTP(t, ln, false, got)

	prev := tlsConfigForTest
	tlsConfigForTest = &tls.Config{InsecureSkipVerify: true, ServerName: "127.0.0.1"}
	t.Cleanup(func() { tlsConfigForTest = prev })

	err = Send(context.Background(), Settings{
		Host: "127.0.0.1", Port: portOf(ln), Security: SecurityTLS, From: "news@example.org",
	}, Message{To: []string{"a@example.org"}, Subject: "ssl", Body: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	body := <-got
	if !strings.Contains(body, "Subject: ssl") || strings.Contains(body, "STARTTLS") {
		t.Fatalf("transcript:\n%s", body)
	}
}

func TestSendRequiresSTARTTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveSMTP(t, ln, false, nil)
	err = Send(context.Background(), Settings{
		Host: "127.0.0.1", Port: portOf(ln), Security: SecuritySTARTTLS, From: "news@example.org",
	}, Message{To: []string{"a@b.c"}, Subject: "x", Body: "y"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("got %v", err)
	}
}

func TestSendSTARTTLS(t *testing.T) {
	cert, err := testCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go serveSMTP(t, ln, true, got)

	prev := tlsConfigForTest
	tlsConfigForTest = &tls.Config{InsecureSkipVerify: true, Certificates: []tls.Certificate{cert}}
	t.Cleanup(func() { tlsConfigForTest = prev })

	err = Send(context.Background(), Settings{
		Host: "127.0.0.1", Port: portOf(ln), Security: SecuritySTARTTLS, From: "News <news@example.org>",
	}, Message{To: []string{"a@b.c"}, Subject: "tls", Body: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	body := <-got
	if !strings.Contains(body, "STARTTLS") || !strings.Contains(body, "Subject: tls") {
		t.Fatalf("transcript:\n%s", body)
	}
}

func portOf(ln net.Listener) int {
	return ln.Addr().(*net.TCPAddr).Port
}

func serveSMTP(t *testing.T, ln net.Listener, starttls bool, got chan string) {
	t.Helper()
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var transcript strings.Builder
	err = smtpSession(&transcript, conn, starttls)
	if got != nil {
		if err != nil {
			transcript.WriteString("\nERROR: " + err.Error())
		}
		got <- transcript.String()
	}
}

func smtpSession(transcript *strings.Builder, conn net.Conn, starttls bool) error {
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	if err := writeLine(rw, "220 ready"); err != nil {
		return err
	}
	upgraded := false
	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return err
		}
		transcript.WriteString(line)
		cmd, _, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		switch strings.ToUpper(cmd) {
		case "EHLO", "HELO":
			if err := writeLine(rw, "250-ready"); err != nil {
				return err
			}
			if starttls && !upgraded {
				if err := writeLine(rw, "250-STARTTLS"); err != nil {
					return err
				}
			}
			if err := writeLine(rw, "250 AUTH PLAIN"); err != nil {
				return err
			}
		case "STARTTLS":
			if err := writeLine(rw, "220 go"); err != nil {
				return err
			}
			cert, err := testCert()
			if err != nil {
				return err
			}
			tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
			if err := tc.Handshake(); err != nil {
				return err
			}
			conn = tc
			rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
			upgraded = true
		case "AUTH":
			if err := writeLine(rw, "235 ok"); err != nil {
				return err
			}
		case "MAIL", "RCPT":
			if err := writeLine(rw, "250 ok"); err != nil {
				return err
			}
		case "DATA":
			if err := writeLine(rw, "354 go"); err != nil {
				return err
			}
			for {
				l, err := rw.ReadString('\n')
				if err != nil {
					return err
				}
				transcript.WriteString(l)
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
			}
			if err := writeLine(rw, "250 queued"); err != nil {
				return err
			}
		case "QUIT":
			return writeLine(rw, "221 bye")
		default:
			if err := writeLine(rw, "502 no"); err != nil {
				return err
			}
		}
	}
}

func writeLine(rw *bufio.ReadWriter, s string) error {
	if _, err := rw.WriteString(s + "\r\n"); err != nil {
		return err
	}
	return rw.Flush()
}

func testCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
