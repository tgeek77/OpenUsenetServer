package mail

import (
	"context"
	"fmt"
	"strings"

	"openusenet/internal/config"
)

// Loader is the store method that holds mail settings saved from the admin portal.
type Loader interface {
	GetMailSettings(ctx context.Context) (Settings, bool, error)
}

// ForInpaths prefers inpaths.report.smtp_host when that older block is set.
func ForInpaths(ctx context.Context, st Loader, cfg config.Config) (Settings, error) {
	base, err := Load(ctx, st, cfg)
	if err != nil {
		return Settings{}, err
	}
	return ForSend(base, InpathsOverride(cfg)), nil
}

// Load returns the saved row when one exists, otherwise the config file / environment.
func Load(ctx context.Context, st Loader, cfg config.Config) (Settings, error) {
	if st == nil {
		return FromConfig(cfg), nil
	}
	saved, ok, err := st.GetMailSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	return PreferSaved(saved, ok, FromConfig(cfg)), nil
}

const (
	SecurityAuto     = "auto"
	SecuritySTARTTLS = "starttls"
	SecurityPlain    = "plain"
	SecurityTLS      = "tls"
)

// Settings is outbound SMTP for a traditional mail server (Postfix, sendmail, or a relay).
type Settings struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Security string // auto, starttls, plain, tls
}

// FromConfig reads the mail: block. Empty host means unset.
func FromConfig(cfg config.Config) Settings {
	m := cfg.Mail
	return Settings{
		Host:     m.Host,
		Port:     m.Port,
		Username: m.Username,
		Password: m.Password,
		From:     m.From,
		Security: m.Security,
	}.Normalize()
}

// InpathsOverride is the older inpaths.report.smtp_* block.
// An empty host means "use the shared mail settings".
func InpathsOverride(cfg config.Config) Settings {
	r := cfg.Inpaths.Report
	if strings.TrimSpace(r.SMTPHost) == "" {
		return Settings{}
	}
	port := r.SMTPPort
	if port <= 0 {
		port = 587
	}
	return Settings{
		Host:     r.SMTPHost,
		Port:     port,
		Username: r.SMTPUser,
		Password: r.SMTPPass,
		From:     r.From,
		Security: SecurityAuto,
	}.Normalize()
}

// PreferSaved uses a database row when one has been stored, including a row that clears the host.
func PreferSaved(saved Settings, savedOK bool, fallback Settings) Settings {
	if savedOK {
		return saved.Normalize()
	}
	return fallback.Normalize()
}

// ForSend uses override when its host is set. An empty override From keeps the base From.
func ForSend(base, override Settings) Settings {
	if strings.TrimSpace(override.Host) == "" {
		return base.Normalize()
	}
	out := override.Normalize()
	if strings.TrimSpace(override.From) == "" {
		out.From = strings.TrimSpace(base.From)
	}
	return out
}

// Normalize trims fields and fills the default port for the security mode.
func (s Settings) Normalize() Settings {
	s.Host = strings.TrimSpace(s.Host)
	s.Username = strings.TrimSpace(s.Username)
	s.From = strings.TrimSpace(s.From)
	s.Password = s.Password
	switch strings.ToLower(strings.TrimSpace(s.Security)) {
	case "", SecurityAuto, "opportunistic":
		s.Security = SecurityAuto
	case SecuritySTARTTLS, "start-tls":
		s.Security = SecuritySTARTTLS
	case SecurityPlain, "none", "off":
		s.Security = SecurityPlain
	case SecurityTLS, "ssl", "smtps":
		s.Security = SecurityTLS
	default:
		s.Security = strings.ToLower(strings.TrimSpace(s.Security))
	}
	if s.Port <= 0 {
		switch s.Security {
		case SecuritySTARTTLS:
			s.Port = 587
		case SecurityTLS:
			s.Port = 465
		default:
			s.Port = 25
		}
	}
	return s
}

// Validate checks a normalized or raw settings value. An empty host is allowed (mail disabled).
func (s Settings) Validate() error {
	s = s.Normalize()
	switch s.Security {
	case SecurityAuto, SecuritySTARTTLS, SecurityPlain, SecurityTLS:
	default:
		return fmt.Errorf("security must be auto, starttls, plain, or tls")
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("port must be 1-65535")
	}
	if s.Host != "" {
		if strings.ContainsAny(s.Host, " \t\r\n/") || strings.Contains(s.Host, "://") {
			return fmt.Errorf("host must be a hostname or address, without a scheme")
		}
	}
	if strings.ContainsAny(s.Username, "\r\n") {
		return fmt.Errorf("username must be a single line")
	}
	if s.From != "" {
		if _, err := Envelope(s.From); err != nil {
			return err
		}
	}
	return nil
}

// Envelope extracts the addr-spec from a bare address or Name <addr> form.
func Envelope(from string) (string, error) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", fmt.Errorf("from address is required")
	}
	if strings.ContainsAny(from, "\r\n") {
		return "", fmt.Errorf("from address must be a single line")
	}
	addr := from
	if i := strings.LastIndex(from, "<"); i >= 0 && strings.HasSuffix(from, ">") {
		addr = strings.TrimSpace(from[i+1 : len(from)-1])
	}
	if !strings.Contains(addr, "@") || strings.ContainsAny(addr, " \t<>") {
		return "", fmt.Errorf("from address must contain @")
	}
	return addr, nil
}
