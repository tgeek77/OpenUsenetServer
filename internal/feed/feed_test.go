package feed

import (
	"testing"

	"github.com/openusenet/openusenet/internal/config"
)

func TestSkipPeer(t *testing.T) {
	cfg := config.Config{
		Server: config.Server{Hostname: "news-a", Pathhost: "news-a"},
	}
	path := "news-a!not-for-mail"
	if !skipPeer(cfg, config.Peer{Host: "news-a"}, path) {
		t.Fatal("should skip self")
	}
	if skipPeer(cfg, config.Peer{Host: "news-b"}, path) {
		t.Fatal("should offer to news-b")
	}
	if !skipPeer(cfg, config.Peer{Host: "news-b"}, "news-b!news-a!not-for-mail") {
		t.Fatal("should skip hop already in Path")
	}
	if skipPeer(cfg, config.Peer{Host: ""}, path) == false {
		t.Fatal("empty host should skip")
	}
}
