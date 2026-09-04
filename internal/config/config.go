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
	Cleanfeed    Cleanfeed    `yaml:"cleanfeed"`
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

// Retention controls article lifetime, binary-flood quotas, and user upload limits.
// default_live_days 0 means keep forever (text default).
type Retention struct {
	DefaultLiveDays       int    `yaml:"default_live_days"` // 0 = forever
	LiveDays              int    `yaml:"live_days"`         // deprecated alias for default_live_days
	FloodLiveDays         int    `yaml:"flood_live_days"`
	HistoryDays           int    `yaml:"history_days"`
	UserBinaryPostsPerDay int    `yaml:"user_binary_posts_per_day"` // 0 disables
	SeedBinaryWildmat     string `yaml:"seed_binary_wildmat"`
	Flood                 Flood  `yaml:"flood"`
}

// Flood thresholds auto-apply FloodLiveDays retention to a group.
type Flood struct {
	WindowHours    int     `yaml:"window_hours"`
	MinBinary      int     `yaml:"min_binary"`
	MinBinaryRatio float64 `yaml:"min_binary_ratio"`
}

// EffectiveDefaultLiveDays returns the configured default (0 = forever).
func (r Retention) EffectiveDefaultLiveDays() int {
	if r.DefaultLiveDays > 0 {
		return r.DefaultLiveDays
	}
	if r.LiveDays > 0 && r.DefaultLiveDays == 0 {
		// Only treat live_days as default when default_live_days was omitted and
		// an explicit positive live_days remains from older configs.
		return 0
	}
	return r.DefaultLiveDays
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
	Open            *bool    `yaml:"open"`
	Allow           []string `yaml:"allow"` // hostnames and/or IP/CIDR
	RequirePeerAuth *bool    `yaml:"require_peer_auth"`
}

// PeerAuthRequired reports whether IHAVE must come from a configured peer with AUTHINFO.
func (i Inbound) PeerAuthRequired() bool {
	if i.RequirePeerAuth == nil {
		return false
	}
	return *i.RequirePeerAuth
}

// Cleanfeed runs cleanfeed-ng through an external command (see scripts/cleanfeed-filter.pl).
type Cleanfeed struct {
	Enabled    bool   `yaml:"enabled"`
	Mode       string `yaml:"mode"` // reject or audit
	Command    string `yaml:"command"`
	Script     string `yaml:"script"`     // cleanfeed-ng Perl script (CLEANFEED_SCRIPT)
	ConfigDir  string `yaml:"config_dir"` // cleanfeed.local directory (CLEANFEED_CONFIG_DIR)
}

func (c Cleanfeed) Reject() bool {
	return c.Enabled && strings.EqualFold(strings.TrimSpace(c.Mode), "reject")
}

func (c Cleanfeed) Audit() bool {
	return c.Enabled && strings.EqualFold(strings.TrimSpace(c.Mode), "audit")
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
		Retention: Retention{
			DefaultLiveDays:       0,
			FloodLiveDays:         7,
			HistoryDays:           30,
			UserBinaryPostsPerDay: 25,
			SeedBinaryWildmat:     "*.bina*,*.bain*,*.dateien*,*.pictures*,alt.binaries.*",
			Flood: Flood{
				WindowHours:    24,
				MinBinary:      20,
				MinBinaryRatio: 0.5,
			},
		},
		Limits: Limits{MaxArtSize: 5_000_000, IdleSeconds: 180},
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
	if cfg.Retention.FloodLiveDays <= 0 {
		cfg.Retention.FloodLiveDays = 7
	}
	if cfg.Retention.HistoryDays <= 0 {
		cfg.Retention.HistoryDays = 30
	}
	if cfg.Retention.UserBinaryPostsPerDay < 0 {
		cfg.Retention.UserBinaryPostsPerDay = 25
	}
	if cfg.Retention.Flood.WindowHours <= 0 {
		cfg.Retention.Flood.WindowHours = 24
	}
	if cfg.Retention.Flood.MinBinary <= 0 {
		cfg.Retention.Flood.MinBinary = 20
	}
	if cfg.Retention.Flood.MinBinaryRatio <= 0 {
		cfg.Retention.Flood.MinBinaryRatio = 0.5
	}
	if strings.TrimSpace(cfg.Retention.SeedBinaryWildmat) == "" {
		cfg.Retention.SeedBinaryWildmat = "*.bina*,*.bain*,*.dateien*,*.pictures*,alt.binaries.*"
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
	cfg.Cleanfeed.Mode = strings.TrimSpace(cfg.Cleanfeed.Mode)
	if cfg.Cleanfeed.Enabled && cfg.Cleanfeed.Mode == "" {
		cfg.Cleanfeed.Mode = "reject"
	}
	cfg.fillCleanfeedDefaults()
	return cfg, nil
}

func (c *Config) fillCleanfeedDefaults() {
	if strings.TrimSpace(c.Cleanfeed.Command) == "" {
		c.Cleanfeed.Command = "perl /usr/local/lib/openusenet/cleanfeed-filter.pl"
	}
	if strings.TrimSpace(c.Cleanfeed.Script) == "" {
		c.Cleanfeed.Script = "/usr/local/lib/cleanfeed-ng/cleanfeed"
	}
	if strings.TrimSpace(c.Cleanfeed.ConfigDir) == "" {
		c.Cleanfeed.ConfigDir = "/usr/local/lib/cleanfeed-ng/etc"
	}
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
	if v := os.Getenv("OPENUSENET_REQUIRE_PEER_AUTH"); v != "" {
		on := v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		cfg.Inbound.RequirePeerAuth = &on
	}
	if v := os.Getenv("OPENUSENET_CLEANFEED"); v != "" {
		cfg.Cleanfeed.Enabled = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v := os.Getenv("OPENUSENET_CLEANFEED_MODE"); v != "" {
		cfg.Cleanfeed.Mode = v
	}
	if v := os.Getenv("OPENUSENET_CLEANFEED_SCRIPT"); v != "" {
		cfg.Cleanfeed.Script = v
	}
	if v := os.Getenv("OPENUSENET_CLEANFEED_CONFIG_DIR"); v != "" {
		cfg.Cleanfeed.ConfigDir = v
	}
	if v := os.Getenv("OPENUSENET_CLEANFEED_COMMAND"); v != "" {
		cfg.Cleanfeed.Command = v
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
