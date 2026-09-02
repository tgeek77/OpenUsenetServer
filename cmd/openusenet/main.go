package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/openusenet/openusenet/internal/archive"
	"github.com/openusenet/openusenet/internal/auth"
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
	case "user":
		return cmdUser(args[1:])
	case "archive":
		return cmdArchive(args[1:])
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
  serve         Run the NNTP server and admin portal
  migrate       Apply PostgreSQL schema and seed groups
  user          Manage users (add first admin, etc.)
  archive       Export mbox.gz snapshots (never deletes live articles)
  healthcheck   Dial NNTP and check the greeting (for Docker HEALTHCHECK)
  version       Print version

Examples:
  openusenet serve --config config.yml
  openusenet user add --admin --username admin --password secret --config config.yml
  openusenet archive export --groups 'misc.test*' --config config.yml
  OPENUSENET_BOOTSTRAP_ADMIN=admin:secret openusenet serve --config config.yml

Use openusenet <command> --help for command options.
`

func cmdServe(args []string) error {
	fs := newFlagSet("serve")
	cfgPath := fs.String("config", "", "Path to config.yml")
	listen := fs.String("listen", "", "NNTP listen address (default :119 or config)")
	pg := fs.String("postgres", "", "PostgreSQL URL")
	mbox := fs.String("mbox-dir", "", "Directory for per-group live mbox spool")
	hostname := fs.String("hostname", "", "Server hostname / Path token")
	httpAddr := fs.String("http", "", "Admin HTTP listen address (default :8080; - to disable)")
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
	if *httpAddr != "" {
		cfg.Listen.HTTP = *httpAddr
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

Run the NNTP reader (RFC 3977) on --listen and the admin portal on --http.
Anonymous NNTP read is allowed. POST requires AUTHINFO once any user exists.
Create the first admin with: openusenet user add --admin ...
or OPENUSENET_BOOTSTRAP_ADMIN=user:pass

Options:
  --config FILE       YAML config (env vars override)
  --listen ADDR       NNTP bind address (default :119)
  --http ADDR         Admin HTTP bind (default :8080; use - to disable)
  --postgres URL      PostgreSQL connection URL
  --mbox-dir DIR      Live per-newsgroup mbox spool directory
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
	if err := server.SeedPeers(ctx, st, cfg); err != nil {
		return err
	}
	if err := server.BootstrapAdmin(ctx, st, log.Default()); err != nil {
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
`

func cmdUser(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(userHelp)
		return nil
	}
	if args[0] != "add" {
		return fmt.Errorf("unknown user subcommand %q\n  openusenet user --help", args[0])
	}
	fs := newFlagSet("user add")
	cfgPath := fs.String("config", "", "Path to config.yml")
	pg := fs.String("postgres", "", "PostgreSQL URL")
	username := fs.String("username", "", "Username")
	password := fs.String("password", "", "Password")
	adminFlag := fs.String("admin", "", "Set to 1/true to create an admin")
	if err := parseHelp(fs, args[1:], userHelp); err != nil {
		return err
	}
	// also accept bare --admin with no value via presence in raw args
	isAdmin := isTruthy(*adminFlag)
	for _, a := range args[1:] {
		if a == "--admin" {
			isAdmin = true
		}
	}
	if *username == "" || *password == "" {
		return fmt.Errorf(" --username and --password are required\n  openusenet user --help")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *pg != "" {
		cfg.Storage.Postgres = *pg
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := store.OpenPostgres(ctx, cfg.Storage.Postgres)
	if err != nil {
		return err
	}
	defer st.Close()
	hash, err := auth.HashPassword(*password)
	if err != nil {
		return err
	}
	role := store.RoleUser
	if isAdmin {
		role = store.RoleAdmin
	}
	u, err := st.CreateUser(ctx, store.User{
		Username: *username, PasswordHash: hash, Role: role, CanPost: true,
	})
	if err != nil {
		return err
	}
	fmt.Printf("ok created %s (%s)\n", u.Username, u.Role)
	return nil
}

const userHelp = `Usage:
  openusenet user add --username NAME --password PASS [--admin] [options]

Create a user for NNTP AUTHINFO and (if --admin) the web portal.

Options:
  --config FILE     YAML config
  --postgres URL    PostgreSQL connection URL
  --username NAME   Login name
  --password PASS   Password
  --admin           Create an admin account

Examples:
  openusenet user add --admin --username admin --password secret --config config.yml
`

func cmdArchive(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(archiveHelp)
		return nil
	}
	if args[0] != "export" {
		return fmt.Errorf("unknown archive subcommand %q\n  openusenet archive --help", args[0])
	}
	fs := newFlagSet("archive export")
	cfgPath := fs.String("config", "", "Path to config.yml")
	pg := fs.String("postgres", "", "PostgreSQL URL")
	groups := fs.String("groups", "all", "all or wildmat")
	dir := fs.String("dir", "", "Export root directory")
	if err := parseHelp(fs, args[1:], archiveHelp); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *pg != "" {
		cfg.Storage.Postgres = *pg
	}
	if *dir != "" {
		cfg.Archive.ExportDir = *dir
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	st, err := store.OpenPostgres(ctx, cfg.Storage.Postgres)
	if err != nil {
		return err
	}
	defer st.Close()
	res, err := archive.Export(ctx, st, cfg.Archive.ExportDir, *groups)
	if err != nil {
		return err
	}
	_ = archive.PruneOldExports(cfg.Archive.ExportDir, cfg.Archive.RetainGens)
	fmt.Printf("ok exported %d groups (%d articles) to %s\n", res.Groups, res.Articles, res.Dir)
	return nil
}

const archiveHelp = `Usage:
  openusenet archive export [--groups all|wildmat] [options]

Write per-newsgroup mboxrd files compressed with gzip. Does not delete
live articles from PostgreSQL.

Options:
  --config FILE     YAML config
  --postgres URL    PostgreSQL connection URL
  --groups SEL      all (default) or wildmat such as misc.test*
  --dir DIR         Export root (default archive.export_dir)

Examples:
  openusenet archive export --groups all --config config.yml
  openusenet archive export --groups 'misc.test*' --dir ./exports
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

func isTruthy(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "1" || v == "true" || v == "yes" || v == "admin"
}

type flagSet struct {
	name string
	args map[string]*string
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
		if a == "--admin" {
			// boolean flag with optional value handled by caller
			if p, ok := f.args["admin"]; ok && (i+1 >= len(args) || strings.HasPrefix(args[i+1], "-")) {
				*p = "1"
				continue
			}
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
