package inbound

import (
	"testing"

	"openusenet/internal/config"
)

func TestAllowedEmptyAndCIDR(t *testing.T) {
	cfg := config.Config{}
	if !Allowed(cfg, "1.2.3.4:9", nil) {
		t.Fatal("empty allow and no peers should permit")
	}
	cfg.Inbound.Allow = []string{"10.0.0.0/8", "news-b"}
	if !Allowed(cfg, "10.1.2.3:119", nil) {
		t.Fatal("cidr")
	}
	if !Allowed(cfg, "news-b:119", nil) {
		t.Fatal("hostname")
	}
	if Allowed(cfg, "8.8.8.8:119", nil) {
		t.Fatal("should deny")
	}
}

func TestAllowedPeersOnly(t *testing.T) {
	cfg := config.Config{}
	peers := []string{"news-b", "news-c"}
	if Allowed(cfg, "8.8.8.8:119", peers) {
		t.Fatal("random IP should deny when peers configured")
	}
	if !Allowed(cfg, "news-b:119", peers) {
		t.Fatal("configured peer hostname should allow")
	}
}

func TestAllowedExplicitClosed(t *testing.T) {
	off := false
	cfg := config.Config{Inbound: config.Inbound{Open: &off}}
	if Allowed(cfg, "1.2.3.4:9", nil) {
		t.Fatal("open:false with no rules should deny")
	}
}

func TestOpen(t *testing.T) {
	cfg := config.Config{}
	if !Open(cfg, nil) {
		t.Fatal("dev default should be open")
	}
	if Open(cfg, []string{"news-a"}) {
		t.Fatal("peers configured should not be wide open")
	}
}
