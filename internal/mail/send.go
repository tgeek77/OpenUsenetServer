package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Message is a plain-text mail.
type Message struct {
	To      []string
	Cc      []string
	Subject string
	Body    string
}

// Send delivers one message with the configured SMTP security mode.
func Send(ctx context.Context, s Settings, msg Message) error {
	s = s.Normalize()
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Host == "" {
		return fmt.Errorf("mail server is not configured")
	}
	fromHeader := s.From
	envelope, err := Envelope(fromHeader)
	if err != nil {
		return err
	}
	to := cleanAddrs(msg.To)
	cc := cleanAddrs(msg.Cc)
	if len(to) == 0 {
		return fmt.Errorf("no recipients")
	}
	subject := strings.TrimSpace(msg.Subject)
	if subject == "" {
		return fmt.Errorf("subject is required")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("subject must be a single line")
	}
	raw := buildMessage(fromHeader, to, cc, subject, msg.Body)
	return sendBytes(ctx, s, envelope, append(to, cc...), raw)
}

// SendRaw delivers raw SMTP DATA bytes. The envelope sender is Settings.From.
// raw must already be a complete message; this does not add Subject or MIME headers.
func SendRaw(ctx context.Context, s Settings, recipients []string, raw []byte) error {
	s = s.Normalize()
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Host == "" {
		return fmt.Errorf("mail server is not configured")
	}
	envelope, err := Envelope(s.From)
	if err != nil {
		return err
	}
	rcpts := cleanAddrs(recipients)
	if len(rcpts) == 0 {
		return fmt.Errorf("no recipients")
	}
	if strings.TrimSpace(string(raw)) == "" {
		return fmt.Errorf("empty message")
	}
	return sendBytes(ctx, s, envelope, rcpts, raw)
}

func sendBytes(ctx context.Context, s Settings, envelope string, recipients []string, raw []byte) error {
	addr := net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	var conn net.Conn
	var err error
	if s.Security == SecurityTLS {
		conn, err = tlsDial(ctx, dialer, addr, s.Host)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()

	if err := c.Hello(heloName()); err != nil {
		return fmt.Errorf("smtp EHLO: %w", err)
	}
	if err := maybeStartTLS(c, s); err != nil {
		return err
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(envelope); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, rcpt := range recipients {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp DATA end: %w", err)
	}
	return c.Quit()
}

func maybeStartTLS(c *smtp.Client, s Settings) error {
	if s.Security == SecurityPlain || s.Security == SecurityTLS {
		return nil
	}
	ok, _ := c.Extension("STARTTLS")
	if !ok {
		if s.Security == SecuritySTARTTLS {
			return fmt.Errorf("smtp server does not offer STARTTLS")
		}
		return nil
	}
	if err := c.StartTLS(clientTLS(s.Host)); err != nil {
		return fmt.Errorf("smtp STARTTLS: %w", err)
	}
	return nil
}

func tlsDial(ctx context.Context, d *net.Dialer, addr, serverName string) (net.Conn, error) {
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	tc := tls.Client(raw, clientTLS(serverName))
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tc, nil
}

func clientTLS(serverName string) *tls.Config {
	if tlsConfigForTest != nil {
		cfg := tlsConfigForTest.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName = serverName
		}
		return cfg
	}
	return &tls.Config{ServerName: serverName}
}

// tlsConfigForTest is set only by tests that present a private certificate.
var tlsConfigForTest *tls.Config

func heloName() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "localhost"
	}
	return h
}

func cleanAddrs(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

func buildMessage(from string, to, cc []string, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	if len(cc) > 0 {
		b.WriteString("Cc: " + strings.Join(cc, ", ") + "\r\n")
	}
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\r\n") {
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}
