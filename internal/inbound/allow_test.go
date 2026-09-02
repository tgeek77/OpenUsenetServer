package inbound

import (
	"testing"

	"github.com/openusenet/openusenet/internal/config"
)

func TestAllowedEmptyAndCIDR(t *testing.T) {
	cfg := config.Config{}
	if !Allowed(cfg, "1.2.3.4:9") {
		t.Fatal("empty allow should permit")
	}
	cfg.Inbound.Allow = []string{"10.0.0.0/8", "news-b"}
	if !Allowed(cfg, "10.1.2.3:119") {
		t.Fatal("cidr")
	}
	if !Allowed(cfg, "news-b:119") {
		t.Fatal("hostname")
	}
	if Allowed(cfg, "8.8.8.8:119") {
		t.Fatal("should deny")
	}
}
