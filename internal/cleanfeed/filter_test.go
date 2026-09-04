package cleanfeed

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"openusenet/internal/config"
)

func TestMissingCommandReject(t *testing.T) {
	cfg := config.Cleanfeed{Enabled: true, Mode: "reject", Command: "/nonexistent/cleanfeed-filter.pl"}
	r := Check(cfg, []byte("Subject: x\r\nFrom: a\r\nNewsgroups: g\r\nMessage-ID: <1>\r\n\r\nbody\r\n"))
	if !r.Reject || !strings.Contains(r.Reason, "CF-") {
		t.Fatalf("got %#v", r)
	}
}

func TestMissingCommandAudit(t *testing.T) {
	cfg := config.Cleanfeed{Enabled: true, Mode: "audit", Command: "/nonexistent/cleanfeed-filter.pl"}
	r := Check(cfg, []byte("Subject: x\r\nFrom: a\r\nNewsgroups: g\r\nMessage-ID: <1>\r\n\r\nbody\r\n"))
	if r.Reject || !r.Audit {
		t.Fatalf("got %#v", r)
	}
}

func TestExternalAccept(t *testing.T) {
	cfg := config.Cleanfeed{Enabled: true, Mode: "reject", Command: "true"}
	r := Check(cfg, []byte("Subject: x\r\nFrom: a\r\nNewsgroups: g\r\nMessage-ID: <1>\r\n\r\nbody\r\n"))
	if r.Reject || r.Reason != "" {
		t.Fatalf("got %#v", r)
	}
}

func TestExternalReject(t *testing.T) {
	cfg := config.Cleanfeed{Enabled: true, Mode: "reject", Command: "false"}
	r := Check(cfg, []byte("Subject: x\r\nFrom: a\r\nNewsgroups: g\r\nMessage-ID: <1>\r\n\r\nbody\r\n"))
	if !r.Reject || !strings.Contains(r.Reason, "CF-EXTERNAL") {
		t.Fatalf("got %#v", r)
	}
}

func TestCleanfeedFilterScriptExists(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	script := filepath.Join(filepath.Dir(file), "..", "..", "scripts", "cleanfeed-filter.pl")
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "filter_art") {
		t.Fatal("expected cleanfeed-ng bridge script")
	}
}
