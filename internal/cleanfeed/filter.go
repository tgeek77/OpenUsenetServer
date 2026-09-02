package cleanfeed

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/openusenet/openusenet/internal/config"
)

// Result is a cleanfeed-ng filter verdict.
type Result struct {
	Reject bool
	Audit  bool
	Reason string
}

// Check runs cleanfeed-ng via the configured external command.
// When enabled without a resolvable command, articles are rejected in reject mode
// or logged in audit mode.
func Check(cfg config.Cleanfeed, raw []byte) Result {
	if !cfg.Enabled {
		return Result{}
	}
	cmdline, env := resolveCommand(cfg)
	if cmdline == "" {
		r := Result{Reason: "CF-NOFILTER cleanfeed enabled but command not configured"}
		return applyMode(cfg, r)
	}
	if r := runExternal(cmdline, env, raw); r.Reason != "" || r.Reject {
		return applyMode(cfg, r)
	}
	return Result{}
}

func applyMode(cfg config.Cleanfeed, r Result) Result {
	if cfg.Reject() {
		r.Reject = true
		r.Audit = false
		return r
	}
	if cfg.Audit() || (cfg.Enabled && r.Reason != "") {
		r.Audit = true
		r.Reject = false
	}
	return r
}

func resolveCommand(cfg config.Cleanfeed) (string, []string) {
	if v := strings.TrimSpace(os.Getenv("OPENUSENET_CLEANFEED_COMMAND")); v != "" {
		return v, envFor(cfg)
	}
	if v := strings.TrimSpace(cfg.Command); v != "" {
		return v, envFor(cfg)
	}
	for _, candidate := range defaultCommands() {
		parts := strings.Fields(candidate)
		if len(parts) == 0 {
			continue
		}
		if _, err := exec.LookPath(parts[0]); err == nil {
			if len(parts) > 1 {
				if _, err := os.Stat(parts[1]); err == nil {
					return candidate, envFor(cfg)
				}
				continue
			}
			return candidate, envFor(cfg)
		}
	}
	return "", nil
}

func defaultCommands() []string {
	return []string{
		"perl /usr/local/lib/openusenet/cleanfeed-filter.pl",
		"perl /usr/lib/openusenet/cleanfeed-filter.pl",
		filepath.Join("scripts", "cleanfeed-filter.pl"),
	}
}

func envFor(cfg config.Cleanfeed) []string {
	env := os.Environ()
	dir := strings.TrimSpace(cfg.ConfigDir)
	if dir == "" {
		dir = "/usr/local/lib/cleanfeed-ng/etc"
	}
	env = append(env, "CLEANFEED_CONFIG_DIR="+dir)
	script := strings.TrimSpace(cfg.Script)
	if script == "" {
		script = "/usr/local/lib/cleanfeed-ng/cleanfeed"
	}
	env = append(env, "CLEANFEED_SCRIPT="+script)
	return env
}

func runExternal(command string, env []string, raw []byte) Result {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return Result{}
	}
	c := exec.CommandContext(ctx, parts[0], parts[1:]...)
	c.Env = env
	c.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	err := c.Run()
	if err == nil {
		return Result{}
	}
	reason := strings.TrimSpace(stderr.String())
	if reason == "" {
		reason = fmt.Sprintf("CF-EXTERNAL filter exit: %v", err)
	}
	return Result{Reason: reason, Reject: true}
}
