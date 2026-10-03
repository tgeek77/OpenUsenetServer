package inpaths

import (
	"context"
	"fmt"
	"strings"

	"openusenet/internal/mail"
)

// MailOpts configures SMTP submission for TOP1000 reports.
type MailOpts struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Security string // auto, starttls, plain, tls; empty means auto
}

// SendReport emails a ninpaths report.
func SendReport(body, pathhost string, to, cc []string, opts MailOpts) error {
	to = cleanAddrs(to)
	if len(to) == 0 {
		return fmt.Errorf("no recipients")
	}
	cc = cleanAddrs(cc)
	from := strings.TrimSpace(opts.From)
	if from == "" {
		from = "openusenet@" + pathhost
	}
	host := strings.TrimSpace(opts.Host)
	if host == "" {
		return fmt.Errorf("smtp host required to send (set the mail server, or pipe openusenet inpaths report to mail)")
	}
	sec := opts.Security
	if sec == "" {
		sec = mail.SecurityAuto
	}
	return mail.Send(context.Background(), mail.Settings{
		Host: host, Port: opts.Port, Username: opts.Username, Password: opts.Password,
		From: from, Security: sec,
	}, mail.Message{To: to, Cc: cc, Subject: "Path statistics from " + pathhost, Body: body})
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
