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

	"openusenet/internal/archive"
	"openusenet/internal/auth"
	"openusenet/internal/config"
	"openusenet/internal/inpaths"
	"openusenet/internal/isc"
	"openusenet/internal/nntp"
	"openusenet/internal/server"
	"openusenet/internal/store"
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
	case "inpaths":
		return cmdInpaths(args[1:])
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
  archive       Export/import mbox snapshots (never deletes live articles)
  inpaths         TOP1000 path statistics (ninpaths-compatible)
  healthcheck   Dial NNTP and check the greeting (for Docker HEALTHCHECK)
  version       Print version

Examples:
  openusenet serve --config config.yml
  openusenet user add --admin --username admin --password secret --config config.yml
  openusenet archive export --groups 'misc.test*' --config config.yml
  openusenet archive import ~/mail/news.groups.mbox --config config.yml
  openusenet inpaths report --config config.yml
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
	defer func() { _ = st.Close() }()
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
	defer func() { _ = st.Close() }()
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
	defer func() { _ = st.Close() }()
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
	switch args[0] {
	case "export":
		return cmdArchiveExport(args[1:])
	case "import":
		return cmdArchiveImport(args[1:])
	default:
		return fmt.Errorf("unknown archive subcommand %q\n  openusenet archive --help", args[0])
	}
}

func cmdArchiveExport(args []string) error {
	fs := newFlagSet("archive export")
	cfgPath := fs.String("config", "", "Path to config.yml")
	pg := fs.String("postgres", "", "PostgreSQL URL")
	groups := fs.String("groups", "all", "all or wildmat")
	dir := fs.String("dir", "", "Export root directory")
	if err := parseHelp(fs, args, archiveHelp); err != nil {
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
	defer func() { _ = st.Close() }()
	res, err := archive.Export(ctx, st, cfg.Archive.ExportDir, *groups)
	if err != nil {
		return err
	}
	_ = archive.PruneOldExports(cfg.Archive.ExportDir, cfg.Archive.RetainGens)
	fmt.Printf("ok exported %d groups (%d articles) to %s\n", res.Groups, res.Articles, res.Dir)
	return nil
}

func cmdArchiveImport(args []string) error {
	fs := newFlagSet("archive import")
	cfgPath := fs.String("config", "", "Path to config.yml")
	pg := fs.String("postgres", "", "PostgreSQL URL")
	group := fs.String("group", "", "Fallback/restrict newsgroup (default: from filename)")
	restrict := fs.Bool("restrict-group", false, "Store only into --group, ignore other Newsgroups")
	createGroups := fs.Bool("create-groups", true, "Ensure missing newsgroups")
	spool := fs.Bool("spool", false, "Also append to live mbox_dir spool")
	if err := parseHelp(fs, args, archiveHelp); err != nil {
		return err
	}
	files := fs.Args()
	if len(files) == 0 {
		return fmt.Errorf("archive import requires at least one mbox file\n  openusenet archive --help")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *pg != "" {
		cfg.Storage.Postgres = *pg
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	st, err := store.OpenPostgres(ctx, cfg.Storage.Postgres)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	var mb *archive.MBox
	if *spool {
		mb = archive.New(cfg.Storage.MBoxDir)
	}
	var total archive.ImportResult
	total.Groups = map[string]int{}
	for _, path := range files {
		opt := archive.ImportOpts{
			Group:         *group,
			RestrictGroup: *restrict,
			CreateGroups:  *createGroups,
			Spool:         mb,
			Hostname:      cfg.Server.Hostname,
		}
		res, err := archive.ImportFile(ctx, st, path, opt)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		fmt.Printf("%s: scanned=%d imported=%d duplicates=%d skipped=%d\n",
			path, res.Scanned, res.Imported, res.Duplicates, res.Skipped)
		total.Scanned += res.Scanned
		total.Imported += res.Imported
		total.Duplicates += res.Duplicates
		total.Skipped += res.Skipped
		for g, n := range res.Groups {
			total.Groups[g] += n
		}
	}
	fmt.Printf("ok import total scanned=%d imported=%d duplicates=%d skipped=%d groups=%d\n",
		total.Scanned, total.Imported, total.Duplicates, total.Skipped, len(total.Groups))
	return nil
}

const archiveHelp = `Usage:
  openusenet archive export [--groups all|wildmat] [options]
  openusenet archive import FILE [FILE...] [options]

export — Write per-newsgroup mboxrd files compressed with gzip. Does not delete
live articles from PostgreSQL.

import — Load historical articles from mbox / mboxrd files into PostgreSQL.
Skips duplicate Message-IDs. Does not offer articles to peers. Newsgroups come
from each article's Newsgroups header; if missing, the filename
(e.g. alt.fan.usenet.mbox → alt.fan.usenet) or --group is used.

Options:
  --config FILE        YAML config
  --postgres URL       PostgreSQL connection URL
  --groups SEL         export: all (default) or wildmat such as misc.test*
  --dir DIR            export root (default archive.export_dir)
  --group NAME         import: fallback/restrict newsgroup
  --restrict-group     import: store only into --group
  --create-groups      import: create missing groups (default true)
  --spool              import: also append to live mbox_dir

Examples:
  openusenet archive export --groups all --config config.yml
  openusenet archive export --groups 'misc.test*' --dir ./exports
  openusenet archive import ~/temp/news.groups.mbox --config config.yml
  openusenet archive import *.mbox --group alt.fan.usenet --restrict-group
`

func cmdInpaths(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(inpathsHelp)
		return nil
	}
	fs := newFlagSet("inpaths")
	cfgPath := fs.String("config", "", "Path to config.yml")
	sub := args[0]
	rest := args[1:]
	if err := parseHelp(fs, rest, inpathsHelp); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if !cfg.InpathsEnabled() {
		return fmt.Errorf("inpaths not enabled — set inpaths.enabled: true in config.yml")
	}
	dir := cfg.InpathsDir()
	pathhost := cfg.Server.Pathhost
	if pathhost == "" {
		pathhost = cfg.Server.Hostname
	}
	switch sub {
	case "flush":
		lg, err := inpaths.NewLogger(dir)
		if err != nil {
			return err
		}
		path, err := lg.Flush()
		if err != nil {
			return err
		}
		fmt.Println("ok", path)
		return nil
	case "report":
		st, err := inpaths.LoadDumps(dir, 32*24*time.Hour)
		if err != nil {
			return err
		}
		body, err := st.Report(pathhost)
		if err != nil {
			return err
		}
		fmt.Print(body)
		return nil
	case "send":
		st, err := inpaths.LoadDumps(dir, 32*24*time.Hour)
		if err != nil {
			return err
		}
		body, err := st.Report(pathhost)
		if err != nil {
			return err
		}
		if cfg.Inpaths.Report.SMTPHost == "" {
			return fmt.Errorf("inpaths.report.smtp_host required (or pipe openusenet inpaths report to mail)")
		}
		if err := inpaths.SendReport(body, pathhost, cfg.InpathsMailTo(), cfg.Inpaths.Report.MailCC, inpaths.MailOpts{
			Host: cfg.Inpaths.Report.SMTPHost, Port: cfg.Inpaths.Report.SMTPPort,
			Username: cfg.Inpaths.Report.SMTPUser, Password: cfg.Inpaths.Report.SMTPPass,
			From: cfg.Inpaths.Report.From,
		}); err != nil {
			return err
		}
		fmt.Println("ok sent to", cfg.InpathsMailTo())
		return nil
	default:
		return fmt.Errorf("unknown inpaths subcommand %q\n  openusenet inpaths --help", sub)
	}
}

const inpathsHelp = `Usage:
  openusenet inpaths flush|report|send [options]

Record Path headers while serving (inpaths.enabled), then submit statistics
to the TOP1000 project — required by many peers (e.g. Eternal September).
See http://top1000.anthologeek.net/#participate

Subcommands:
  flush     Write a ninpaths dump file from the live accumulator (usually automatic)
  report    Print merged report (pipe to mail for top1000@anthologeek.net)
  send      Email report via configured SMTP

Options:
  --config FILE     YAML config

Example cron (daily flush + mail via sendmail):
  6 6 * * * openusenet inpaths flush --config /etc/openusenet/config.yml
  10 6 * * * openusenet inpaths report --config /etc/openusenet/config.yml | mail -s "inpaths $(hostname)" top1000@anthologeek.net
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
	name  string
	args  map[string]*string
	bools map[string]*bool
	pos   []string
}

func newFlagSet(name string) *flagSet {
	return &flagSet{name: name, args: map[string]*string{}, bools: map[string]*bool{}}
}

func (f *flagSet) String(name, def, _ string) *string {
	s := def
	f.args[name] = &s
	return f.args[name]
}

func (f *flagSet) Bool(name string, def bool, _ string) *bool {
	b := def
	f.bools[name] = &b
	return f.bools[name]
}

func (f *flagSet) Args() []string { return f.pos }

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
			hasEq := false
			if k, v, ok := splitEq(key); ok {
				key, val, hasEq = k, v, true
			}
			if bp, ok := f.bools[key]; ok {
				if hasEq {
					*bp = isTruthy(val)
				} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && (args[i+1] == "true" || args[i+1] == "false" || args[i+1] == "1" || args[i+1] == "0" || args[i+1] == "yes" || args[i+1] == "no") {
					i++
					*bp = isTruthy(args[i])
				} else {
					*bp = true
				}
				continue
			}
			if !hasEq {
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
		f.pos = append(f.pos, a)
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
