package moderate

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"openusenet/internal/mail"
	"openusenet/internal/wildmat"
)

// DefaultAddress is the public submission template. %s is the newsgroup
// with each dot changed to a hyphen, so misc.test.moderated is mailed to
// misc-test-moderated@moderators.isc.org. News servers compute this; they
// do not download a moderator roster.
const DefaultAddress = "%s@moderators.isc.org"

// Rule is one local override. The first matching pattern wins, and the
// public default is used only when none match.
type Rule struct {
	Pattern string
	Address string
}

type senderFunc func(context.Context, mail.Settings, []string, []byte) error

var deliver atomic.Value

func init() {
	deliver.Store(senderFunc(mail.SendRaw))
}

// SetDeliver replaces the SMTP sender until the returned function is called.
// Tests use it. Production mail goes through mail.SendRaw.
func SetDeliver(fn senderFunc) func() {
	prev := deliver.Load().(senderFunc)
	deliver.Store(fn)
	return func() { deliver.Store(prev) }
}

// Target returns the first known moderated group in Newsgroups order.
// An Approved header means the article is already approved and should be stored.
// Unknown groups are skipped. lookup reports known=false when the server has no such group.
func Target(groups []string, approved bool, lookup func(name string) (status string, known bool, err error)) (string, error) {
	if approved {
		return "", nil
	}
	for _, name := range groups {
		status, known, err := lookup(name)
		if err != nil {
			return "", err
		}
		if known && status == "m" {
			return name, nil
		}
	}
	return "", nil
}

// Address resolves the submission mailbox for one moderated group.
func Address(group string, rules []Rule) (string, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return "", fmt.Errorf("moderated group name is required")
	}
	for _, r := range rules {
		if wildmat.Match(r.Pattern, group) {
			return ApplyTemplate(group, r.Address)
		}
	}
	return ApplyTemplate(group, DefaultAddress)
}

// ApplyTemplate expands at most one %s. Dots in the group become hyphens.
// %% is a literal percent. A template with no %s is used as a fixed address.
func ApplyTemplate(group, template string) (string, error) {
	template = strings.TrimSpace(template)
	if template == "" {
		return "", fmt.Errorf("submission address is required")
	}
	dashed := strings.ReplaceAll(strings.TrimSpace(group), ".", "-")
	var b strings.Builder
	seen := false
	for i := 0; i < len(template); i++ {
		if template[i] != '%' {
			b.WriteByte(template[i])
			continue
		}
		if i+1 >= len(template) {
			return "", fmt.Errorf("submission address may contain only %%s or %%%%")
		}
		switch template[i+1] {
		case '%':
			b.WriteByte('%')
			i++
		case 's':
			if seen {
				return "", fmt.Errorf("submission address may contain only one %%s")
			}
			seen = true
			b.WriteString(dashed)
			i++
		default:
			return "", fmt.Errorf("submission address may contain only %%s or %%%%")
		}
	}
	addr := b.String()
	if strings.ContainsAny(addr, " \t\r\n<>") || strings.Count(addr, "@") != 1 {
		return "", fmt.Errorf("submission address must be an email address")
	}
	local, domain, _ := strings.Cut(addr, "@")
	if local == "" || domain == "" {
		return "", fmt.Errorf("submission address must be an email address")
	}
	return addr, nil
}

// NormalizeRule checks a local override before it is stored. The address is
// kept as a template; %s is expanded when a post is mailed.
func NormalizeRule(pattern, address string) (string, string, error) {
	pattern = strings.TrimSpace(pattern)
	address = strings.TrimSpace(address)
	if pattern == "" || strings.ContainsAny(pattern, " \t\r\n,") || strings.HasPrefix(pattern, "!") || strings.HasPrefix(pattern, "@") || !wildmat.Valid(pattern) {
		return "", "", fmt.Errorf("pattern must be a single wildmat")
	}
	if _, err := ApplyTemplate("example.test", address); err != nil {
		return "", "", err
	}
	return pattern, address, nil
}

// Submit resolves the address and mails the article bytes. The caller must
// not store or offer the article when this succeeds.
func Submit(ctx context.Context, settings mail.Settings, group string, rules []Rule, wire []byte) (string, error) {
	addr, err := Address(group, rules)
	if err != nil {
		return "", err
	}
	if err := Mail(ctx, settings, addr, wire); err != nil {
		return "", err
	}
	return addr, nil
}

// Mail sends the article as the message body. A To header is prepended.
// The envelope sender is the configured mail From, not the article From.
// Auto-Submitted is not added; moderator clients treat it as automatic mail.
func Mail(ctx context.Context, settings mail.Settings, to string, wire []byte) error {
	if strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("submission address must be an email address")
	}
	raw := make([]byte, 0, len(to)+len(wire)+8)
	raw = append(raw, "To: "...)
	raw = append(raw, to...)
	raw = append(raw, "\r\n"...)
	raw = append(raw, wire...)
	fn := deliver.Load().(senderFunc)
	return fn(ctx, settings, []string{to}, raw)
}
