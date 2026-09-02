package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openusenet/openusenet/internal/archive"
	"github.com/openusenet/openusenet/internal/config"
	"github.com/openusenet/openusenet/internal/isc"
	"github.com/openusenet/openusenet/internal/nntp"
	"github.com/openusenet/openusenet/internal/server"
	"github.com/openusenet/openusenet/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("openusenet: ")
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(rootHelp)
		return nil
	}
	switch args[0] {
	case "serve":
		return cmdServe(args[1:])
	case "migrate":
		return cmdMigrate(args[1:])
	case "healthcheck":
		return cmdHealth(args[1:])
	case "version":
		fmt.Printf("%s %s\n", nntp.Software, nntp.Version)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n  openusenet --help", args[0])
	}
}

const rootHelp = `OpenUsenetServer — NNTP without inn.conf.

Usage:
  openusenet <command> [options]

Commands:
  serve         Run the NNTP server
  migrate       Apply PostgreSQL schema and seed groups
  healthcheck   Dial NNTP and check the greeting (for Docker HEALTHCHECK)
  version       Print version

Examples:
  openusenet serve --config config.yml
  openusenet serve --listen :1119 --postgres postgres://openusenet:openusenet@127.0.0.1:5432/openusenet?sslmode=disable
  openusenet migrate --config config.yml
  openusenet healthcheck --addr 127.0.0.1:119

Use openusenet <command> --help for command options.
`

func cmdServe(args []string) error {
	fs := newFlagSet("serve")
	cfgPath := fs.String("config", "", "Path to config.yml")
	listen := fs.String("listen", "", "NNTP listen address (default :119 or config)")
	pg := fs.String("postgres", "", "PostgreSQL URL")
	mbox := fs.String("mbox-dir", "", "Directory for per-group mbox archives")
	hostname := fs.String("hostname", "", "Server hostname / Path token")
	if err := parseHelp(fs, args, serveHelp); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.Listen.NNTP = *listen
	}
	if *pg != "" {
		cfg.Storage.Postgres = *pg
	}
	if *mbox != "" {
		cfg.Storage.MBoxDir = *mbox
	}
	if *hostname != "" {
		cfg.Server.Hostname = *hostname
		if cfg.Server.Pathhost == cfg.Server.Hostname || cfg.Server.Pathhost == "" {
			cfg.Server.Pathhost = *hostname
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	st, err := store.OpenPostgres(ctx, cfg.Storage.Postgres)
	if err != nil {
		return fmt.Errorf("%v\n  openusenet serve --postgres postgres://USER:PASS@HOST:5432/DB?sslmode=disable", err)
	}
	defer st.Close()
	if err := seed(ctx, st, cfg, false); err != nil {
		return err
	}
	mb := archive.New(cfg.Storage.MBoxDir)
	srv := server.New(cfg, st, mb, nil)
	return srv.ListenAndServe(ctx)
}

const serveHelp = `Usage:
  openusenet serve [options]

Run the NNTP reader (RFC 3977) on --listen. Creates schema if needed.

Options:
  --config FILE       YAML config (env vars override)
  --listen ADDR       NNTP bind address (default :119)
  --postgres URL      PostgreSQL connection URL
  --mbox-dir DIR      Per-newsgroup mbox archive directory
  --hostname NAME     Path / Message-ID hostname

Examples:
  openusenet serve --config config.yml
  openusenet serve --listen :1119 --postgres postgres://openusenet:openusenet@127.0.0.1:5432/openusenet?sslmode=disable
`

func cmdMigrate(args []string) error {
	fs := newFlagSet("migrate")
	cfgPath := fs.String("config", "", "Path to config.yml")
	pg := fs.String("postgres", "", "PostgreSQL URL")
	if err := parseHelp(fs, args, migrateHelp); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *pg != "" {
		cfg.Storage.Postgres = *pg
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	st, err := store.OpenPostgres(ctx, cfg.Storage.Postgres)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := seed(ctx, st, cfg, true); err != nil {
		return err
	}
	n, err := st.CountGroups(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("ok schema and %d groups\n", n)
	return nil
}

const migrateHelp = `Usage:
  openusenet migrate [options]

Apply the PostgreSQL schema (idempotent), pull the ISC active/newsgroups
list, and seed any extra groups from config.yml.

Options:
  --config FILE     YAML config
  --postgres URL    PostgreSQL connection URL

Examples:
  openusenet migrate --config config.yml
  openusenet migrate --postgres postgres://openusenet:openusenet@127.0.0.1:5432/DB?sslmode=disable
`

func cmdHealth(args []string) error {
	fs := newFlagSet("healthcheck")
	addr := fs.String("addr", "127.0.0.1:119", "NNTP address to dial")
	if err := parseHelp(fs, args, healthHelp); err != nil {
		return err
	}
	if err := server.Healthcheck(*addr, 3*time.Second); err != nil {
		return fmt.Errorf("%v\n  openusenet healthcheck --addr 127.0.0.1:119", err)
	}
	fmt.Println("ok")
	return nil
}

const healthHelp = `Usage:
  openusenet healthcheck [options]

Dial NNTP and check for a 200/201 greeting. Exit 0 on success.

Options:
  --addr HOST:PORT   Address (default 127.0.0.1:119)

Examples:
  openusenet healthcheck --addr 127.0.0.1:119
  openusenet healthcheck --addr 127.0.0.1:1119
`

func seed(ctx context.Context, st store.Store, cfg config.Config, alwaysISC bool) error {
	if cfg.ShouldFetchISC() {
		n, err := st.CountGroups(ctx)
		if err != nil {
			return err
		}
		if alwaysISC || n == 0 {
			groups, err := isc.Fetch(ctx, cfg.GroupsSource.ISCURL)
			if err != nil {
				return fmt.Errorf("isc newsgroups: %w", err)
			}
			if err := st.EnsureGroups(ctx, groups); err != nil {
				return fmt.Errorf("isc newsgroups store: %w", err)
			}
			log.Printf("loaded %d groups from ISC", len(groups))
		}
	}
	for _, g := range cfg.Groups {
		if err := st.EnsureGroup(ctx, g.Name, g.Description, g.Status); err != nil {
			return fmt.Errorf("seed group %s: %w", g.Name, err)
		}
	}
	return nil
}

type flagSet struct {
	name string
	args map[string]*string
	raw  []string
}

func newFlagSet(name string) *flagSet {
	return &flagSet{name: name, args: map[string]*string{}}
}

func (f *flagSet) String(name, def, _ string) *string {
	s := def
	f.args[name] = &s
	return f.args[name]
}

func parseHelp(f *flagSet, args []string, help string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-h" || a == "--help" {
			fmt.Print(help)
			os.Exit(0)
		}
		if len(a) >= 2 && a[:2] == "--" {
			key := a[2:]
			val := ""
			if k, v, ok := splitEq(key); ok {
				key, val = k, v
			} else {
				if i+1 >= len(args) {
					return fmt.Errorf("missing value for --%s\n  openusenet %s --help", key, f.name)
				}
				i++
				val = args[i]
			}
			p, ok := f.args[key]
			if !ok {
				return fmt.Errorf("unknown flag --%s\n  openusenet %s --help", key, f.name)
			}
			*p = val
			continue
		}
		return fmt.Errorf("unexpected argument %q\n  openusenet %s --help", a, f.name)
	}
	return nil
}

func splitEq(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
