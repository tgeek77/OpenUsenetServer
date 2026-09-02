package inpaths

import (
	"fmt"
	"net/smtp"
	"strings"
)

// MailOpts configures SMTP submission for TOP1000 reports.
type MailOpts struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// SendReport emails a ninpaths report.
func SendReport(body, pathhost string, to, cc []string, opts MailOpts) error {
	to = cleanAddrs(to)
	if len(to) == 0 {
		return fmt.Errorf("no recipients")
	}
	cc = cleanAddrs(cc)
	all := append(append([]string{}, to...), cc...)
	from := strings.TrimSpace(opts.From)
	if from == "" {
		from = "openusenet@" + pathhost
	}
	subject := "Path statistics from " + pathhost
	msg := buildMessage(from, to, cc, subject, body)
	host := strings.TrimSpace(opts.Host)
	if host == "" {
		return fmt.Errorf("smtp_host required to send (use openusenet inpaths report and pipe to mail)")
	}
	port := opts.Port
	if port <= 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	var auth smtp.Auth
	if opts.Username != "" {
		auth = smtp.PlainAuth("", opts.Username, opts.Password, host)
	}
	return smtp.SendMail(addr, auth, from, all, []byte(msg))
}

func cleanAddrs(in []string) []string {
	var out []string
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

func buildMessage(from string, to, cc []string, subject, body string) string {
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
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}
