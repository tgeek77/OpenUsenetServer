package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the native OpenUsenetServer configuration. It is not inn.conf.
type Config struct {
	Server    Server    `yaml:"server"`
	Listen    Listen    `yaml:"listen"`
	Storage   Storage   `yaml:"storage"`
	Retention Retention `yaml:"retention"`
	Limits    Limits    `yaml:"limits"`
	Groups    []Group   `yaml:"groups"`
}

type Server struct {
	Hostname     string `yaml:"hostname"`
	Organization string `yaml:"organization"`
	Pathhost     string `yaml:"pathhost"`
}

type Listen struct {
	NNTP string `yaml:"nntp"`
}

type Storage struct {
	Postgres string `yaml:"postgres"`
	MBoxDir  string `yaml:"mbox_dir"`
}

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

func Defaults() Config {
	return Config{
		Server: Server{
			Hostname:     "news.localhost",
			Organization: "OpenUsenetServer",
		},
		Listen: Listen{NNTP: ":119"},
		Storage: Storage{
			Postgres: "postgres://openusenet:openusenet@127.0.0.1:5432/openusenet?sslmode=disable",
			MBoxDir:  "./archive",
		},
		Retention: Retention{LiveDays: 30, HistoryDays: 60},
		Limits:    Limits{MaxArtSize: 5_000_000, IdleSeconds: 180},
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
	for i := range cfg.Groups {
		if cfg.Groups[i].Status == "" {
			cfg.Groups[i].Status = "y"
		}
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
	if v := os.Getenv("OPENUSENET_POSTGRES"); v != "" {
		cfg.Storage.Postgres = v
	}
	if v := os.Getenv("OPENUSENET_MBOX_DIR"); v != "" {
		cfg.Storage.MBoxDir = v
	}
	if v := os.Getenv("OPENUSENET_MAX_ART_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Limits.MaxArtSize = n
		}
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
