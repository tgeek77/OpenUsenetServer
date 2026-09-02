package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the native OpenUsenetServer configuration. It is not inn.conf.
type Config struct {
	Server       Server       `yaml:"server"`
	Listen       Listen       `yaml:"listen"`
	TLS          TLS          `yaml:"tls"`
	Storage      Storage      `yaml:"storage"`
	Retention    Retention    `yaml:"retention"`
	Limits       Limits       `yaml:"limits"`
	Groups       []Group      `yaml:"groups"`
	Peers        []Peer       `yaml:"peers"`
	GroupsSource GroupsSource `yaml:"groups_source"`
	Inbound      Inbound      `yaml:"inbound"`
	Inpaths      Inpaths      `yaml:"inpaths"`
	Archive      Archive      `yaml:"archive"`
}

type Server struct {
	Hostname     string `yaml:"hostname"`
	Organization string `yaml:"organization"`
	Pathhost     string `yaml:"pathhost"`
}

type Listen struct {
	NNTP    string `yaml:"nntp"`
	HTTP    string `yaml:"http"`     // admin portal; "-" disables
	NNTPTLS string `yaml:"nntp_tls"` // optional TLS NNTP; empty disables
	HTTPTLS string `yaml:"http_tls"` // optional TLS admin; empty disables
}

type TLS struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

func (t TLS) Enabled() bool {
	return strings.TrimSpace(t.CertFile) != "" && strings.TrimSpace(t.KeyFile) != ""
}

type Storage struct {
	Postgres string `yaml:"postgres"`
	MBoxDir  string `yaml:"mbox_dir"` // live append-on-POST mbox spool
}

// Retention fields are documented but not enforced: live articles are kept indefinitely.
type Retention struct {
	LiveDays    int `yaml:"live_days"`
	HistoryDays int `yaml:"history_days"`
}

type Limits struct {
	MaxArtSize  int `yaml:"max_art_size"`
	IdleSeconds int `yaml:"idle_seconds"`
}

type Group struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Status      string `yaml:"status"`
}

// Peer is a YAML seed for outbound IHAVE destinations (copied into DB on first boot).
type Peer struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

func (p Peer) Addr() string {
	port := p.Port
	if port <= 0 {
		port = 119
	}
	return net.JoinHostPort(p.Host, strconv.Itoa(port))
}

// GroupsSource controls the canonical ISC newsgroup list.
type GroupsSource struct {
	ISCURL   string `yaml:"isc_url"`
	FetchISC *bool  `yaml:"fetch_isc"`
}

// Inbound controls who may IHAVE to this server.
// With open unset/true and no allow list or peers, all remotes may IHAVE (dev default).
// With peers configured, enabled peer hostnames are always allowed (resolved to IP).
// Set open: false to deny everyone not listed in allow or configured as a peer.
type Inbound struct {
	Open  *bool    `yaml:"open"`
	Allow []string `yaml:"allow"` // hostnames and/or IP/CIDR
}

// Inpaths controls TOP1000 path statistics (ninpaths-compatible dumps).
type Inpaths struct {
	Enabled  bool          `yaml:"enabled"`
	Dir      string        `yaml:"dir"`      // contains path/ with inpaths.* dumps
	Schedule string        `yaml:"schedule"` // daily flush+report; empty = manual only
	Report   InpathsReport `yaml:"report"`
}

type InpathsReport struct {
	MailTo   []string `yaml:"mailto"` // default top1000@anthologeek.net when sending
	MailCC   []string `yaml:"cc"`
	SMTPHost string   `yaml:"smtp_host"`
	SMTPPort int      `yaml:"smtp_port"`
	SMTPUser string   `yaml:"smtp_user"`
	SMTPPass string   `yaml:"smtp_pass"`
	From     string   `yaml:"from"`
}

// InpathsEnabled reports whether path logging is on.
func (c Config) InpathsEnabled() bool {
	if c.Inpaths.Enabled {
		return true
	}
	return strings.TrimSpace(c.Inpaths.Dir) != ""
}

func (c Config) InpathsDir() string {
	dir := strings.TrimSpace(c.Inpaths.Dir)
	if dir == "" {
		dir = "./pathlog"
	}
	return dir
}

func (c Config) InpathsMailTo() []string {
	if len(c.Inpaths.Report.MailTo) > 0 {
		return c.Inpaths.Report.MailTo
	}
	return []string{"top1000@anthologeek.net"}
}

// Archive controls export snapshots (mbox.gz). Never deletes live articles.
type Archive struct {
	ExportDir   string `yaml:"export_dir"`
	Schedule    string `yaml:"schedule"` // daily, weekly, or empty to disable
	Groups      string `yaml:"groups"`   // all or wildmat
	RetainGens  int    `yaml:"retain_generations"`
}

const DefaultISCURL = "https://ftp.isc.org/usenet/CONFIG"

func (c Config) ShouldFetchISC() bool {
	if c.GroupsSource.FetchISC == nil {
		return true
	}
	return *c.GroupsSource.FetchISC
}

func Defaults() Config {
	return Config{
		Server: Server{
			Hostname:     "news.localhost",
			Organization: "OpenUsenetServer",
		},
		Listen: Listen{NNTP: ":119", HTTP: ":8080"},
		Storage: Storage{
			Postgres: "postgres://openusenet:openusenet@127.0.0.1:5432/openusenet?sslmode=disable",
			MBoxDir:  "./archive",
		},
		Retention: Retention{LiveDays: 30, HistoryDays: 60},
		Limits:    Limits{MaxArtSize: 5_000_000, IdleSeconds: 180},
		GroupsSource: GroupsSource{
			ISCURL: DefaultISCURL,
		},
		Archive: Archive{
			ExportDir:  "./exports",
			Groups:     "all",
			RetainGens: 4,
		},
		Groups: []Group{{
			Name:        "local.test",
			Description: "Local test group",
			Status:      "y",
		}},
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	applyEnv(&cfg)
	if cfg.Server.Pathhost == "" {
		cfg.Server.Pathhost = cfg.Server.Hostname
	}
	if cfg.Server.Hostname == "" {
		return Config{}, fmt.Errorf("server.hostname is required\n  openusenet serve --hostname news.example.org")
	}
	if cfg.Listen.NNTP == "" {
		cfg.Listen.NNTP = ":119"
	}
	if cfg.Listen.HTTP == "" {
		cfg.Listen.HTTP = ":8080"
	}
	if cfg.Limits.MaxArtSize <= 0 {
		cfg.Limits.MaxArtSize = 5_000_000
	}
	if cfg.Limits.IdleSeconds <= 0 {
		cfg.Limits.IdleSeconds = 180
	}
	if cfg.Retention.LiveDays <= 0 {
		cfg.Retention.LiveDays = 30
	}
	if cfg.Retention.HistoryDays <= 0 {
		cfg.Retention.HistoryDays = cfg.Retention.LiveDays * 2
	}
	if strings.TrimSpace(cfg.Archive.ExportDir) == "" {
		cfg.Archive.ExportDir = "./exports"
	}
	if strings.TrimSpace(cfg.Archive.Groups) == "" {
		cfg.Archive.Groups = "all"
	}
	if cfg.Archive.RetainGens <= 0 {
		cfg.Archive.RetainGens = 4
	}
	for i := range cfg.Groups {
		if cfg.Groups[i].Status == "" {
			cfg.Groups[i].Status = "y"
		}
	}
	var peers []Peer
	for _, p := range cfg.Peers {
		p.Host = strings.TrimSpace(p.Host)
		if p.Host == "" {
			continue
		}
		if p.Port <= 0 {
			p.Port = 119
		}
		peers = append(peers, p)
	}
	cfg.Peers = peers
	if strings.TrimSpace(cfg.GroupsSource.ISCURL) == "" {
		cfg.GroupsSource.ISCURL = DefaultISCURL
	}
	var allow []string
	for _, a := range cfg.Inbound.Allow {
		a = strings.TrimSpace(a)
		if a != "" {
			allow = append(allow, a)
		}
	}
	cfg.Inbound.Allow = allow
	if strings.TrimSpace(cfg.Inpaths.Dir) == "" && cfg.Inpaths.Enabled {
		cfg.Inpaths.Dir = "./pathlog"
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("OPENUSENET_HOSTNAME"); v != "" {
		cfg.Server.Hostname = v
	}
	if v := os.Getenv("OPENUSENET_ORGANIZATION"); v != "" {
		cfg.Server.Organization = v
	}
	if v := os.Getenv("OPENUSENET_PATHHOST"); v != "" {
		cfg.Server.Pathhost = v
	}
	if v := os.Getenv("OPENUSENET_LISTEN"); v != "" {
		cfg.Listen.NNTP = v
	}
	if v := os.Getenv("OPENUSENET_HTTP"); v != "" {
		cfg.Listen.HTTP = v
	}
	if v := os.Getenv("OPENUSENET_NNTP_TLS"); v != "" {
		cfg.Listen.NNTPTLS = v
	}
	if v := os.Getenv("OPENUSENET_HTTP_TLS"); v != "" {
		cfg.Listen.HTTPTLS = v
	}
	if v := os.Getenv("OPENUSENET_TLS_CERT"); v != "" {
		cfg.TLS.CertFile = v
	}
	if v := os.Getenv("OPENUSENET_TLS_KEY"); v != "" {
		cfg.TLS.KeyFile = v
	}
	if v := os.Getenv("OPENUSENET_POSTGRES"); v != "" {
		cfg.Storage.Postgres = v
	}
	if v := os.Getenv("OPENUSENET_MBOX_DIR"); v != "" {
		cfg.Storage.MBoxDir = v
	}
	if v := os.Getenv("OPENUSENET_EXPORT_DIR"); v != "" {
		cfg.Archive.ExportDir = v
	}
	if v := os.Getenv("OPENUSENET_ISC_URL"); v != "" {
		cfg.GroupsSource.ISCURL = v
	}
	if v := os.Getenv("OPENUSENET_FETCH_ISC"); v != "" {
		on := v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		cfg.GroupsSource.FetchISC = &on
	}
	if v := os.Getenv("OPENUSENET_MAX_ART_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Limits.MaxArtSize = n
		}
	}
	if v := os.Getenv("OPENUSENET_INBOUND_ALLOW"); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.Inbound.Allow = append(cfg.Inbound.Allow, p)
			}
		}
	}
	if v := os.Getenv("OPENUSENET_INBOUND_OPEN"); v != "" {
		on := v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		cfg.Inbound.Open = &on
	}
	if v := os.Getenv("OPENUSENET_INPATHS_DIR"); v != "" {
		cfg.Inpaths.Dir = v
		cfg.Inpaths.Enabled = true
	}
	if v := os.Getenv("OPENUSENET_INPATHS_ENABLE"); v != "" {
		cfg.Inpaths.Enabled = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
}

func (c Config) Idle() time.Duration {
	return time.Duration(c.Limits.IdleSeconds) * time.Second
}

func (c Config) StringListen() string {
	return c.Listen.NNTP
}

func NormalizeListen(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ":119"
	}
	if !strings.Contains(addr, ":") {
		return ":" + addr
	}
	return addr
}
